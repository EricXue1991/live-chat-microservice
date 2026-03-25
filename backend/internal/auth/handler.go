package auth

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"livechat/internal/config"
	"livechat/internal/models"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// Handler manages user authentication against PostgreSQL.
// Users and rooms live in PostgreSQL because they have relational
// properties (profiles, memberships, metadata) suited for SQL joins.
// Chat messages stay in DynamoDB for high-throughput partition-key access.
type Handler struct {
	db  *sql.DB
	cfg *config.Config
}

func NewHandler(db *sql.DB, cfg *config.Config) *Handler {
	return &Handler{db: db, cfg: cfg}
}

// InitSchema creates the users and rooms tables if they don't exist.
// Called once at startup — idempotent via IF NOT EXISTS.
func (h *Handler) InitSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id           SERIAL PRIMARY KEY,
		username     VARCHAR(50) UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		display_name VARCHAR(100) DEFAULT '',
		created_at   TIMESTAMP DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS rooms (
		id          VARCHAR(100) PRIMARY KEY,
		name        VARCHAR(200) NOT NULL,
		description TEXT DEFAULT '',
		created_by  VARCHAR(50) REFERENCES users(username),
		created_at  TIMESTAMP DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS room_members (
		room_id  VARCHAR(100) REFERENCES rooms(id),
		username VARCHAR(50) REFERENCES users(username),
		joined_at TIMESTAMP DEFAULT NOW(),
		PRIMARY KEY (room_id, username)
	);
	`
	_, err := h.db.Exec(schema)
	if err != nil {
		log.Printf("[AUTH] schema init error: %v", err)
	} else {
		log.Println("[AUTH] PostgreSQL schema initialized")
	}

	// Seed default rooms so the frontend sidebar has content
	defaultRooms := []struct{ id, name, desc string }{
		{"room-general", "General", "General discussion"},
		{"room-hot", "Hot Room", "Viral room for experiment 2"},
		{"room-tech", "Tech Talk", "Technology chat"},
		{"room-random", "Random", "Off-topic fun"},
		{"room-music", "Music", "Music discussion"},
	}
	for _, r := range defaultRooms {
		h.db.Exec(`INSERT INTO rooms (id, name, description) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
			r.id, r.name, r.desc)
	}

	return err
}

// Register creates a new user account.
// POST /api/register
// Flow: validate input → check uniqueness → bcrypt hash → INSERT into PostgreSQL.
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req models.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, models.ErrorResponse{Error: "invalid request body"})
		return
	}

	if req.Username == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, models.ErrorResponse{Error: "username and password required"})
		return
	}
	if len(req.Password) < 6 {
		writeJSON(w, http.StatusBadRequest, models.ErrorResponse{Error: "password must be at least 6 characters"})
		return
	}

	// bcrypt hash with cost 10 — secure and performant
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), 10)
	if err != nil {
		log.Printf("[AUTH] bcrypt error: %v", err)
		writeJSON(w, http.StatusInternalServerError, models.ErrorResponse{Error: "internal error"})
		return
	}

	displayName := req.DisplayName
	if displayName == "" {
		displayName = req.Username
	}

	// INSERT into PostgreSQL — UNIQUE constraint on username handles duplicates
	_, err = h.db.Exec(
		`INSERT INTO users (username, password_hash, display_name) VALUES ($1, $2, $3)`,
		req.Username, string(hash), displayName,
	)
	if err != nil {
		// PostgreSQL unique violation code = 23505
		if isPgUniqueViolation(err) {
			writeJSON(w, http.StatusConflict, models.ErrorResponse{Error: "username already exists"})
		} else {
			log.Printf("[AUTH] INSERT error: %v", err)
			writeJSON(w, http.StatusInternalServerError, models.ErrorResponse{Error: "failed to create user"})
		}
		return
	}

	log.Printf("[AUTH] user registered: %s", req.Username)
	writeJSON(w, http.StatusCreated, map[string]string{
		"message":  "user created",
		"username": req.Username,
	})
}

// Login authenticates a user and returns a signed JWT.
// POST /api/login
// Flow: SELECT from PostgreSQL → bcrypt compare → sign JWT (24h expiry).
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req models.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, models.ErrorResponse{Error: "invalid request body"})
		return
	}

	// Query PostgreSQL for user record
	var user models.User
	var passwordHash string
	err := h.db.QueryRow(
		`SELECT id, username, password_hash, display_name FROM users WHERE username = $1`,
		req.Username,
	).Scan(&user.ID, &user.Username, &passwordHash, &user.DisplayName)

	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusUnauthorized, models.ErrorResponse{Error: "invalid credentials"})
		return
	} else if err != nil {
		log.Printf("[AUTH] query error: %v", err)
		writeJSON(w, http.StatusInternalServerError, models.ErrorResponse{Error: "internal error"})
		return
	}

	// Compare bcrypt hash
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		writeJSON(w, http.StatusUnauthorized, models.ErrorResponse{Error: "invalid credentials"})
		return
	}

	// Sign JWT — stateless so any replica can validate without shared session store
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"username":     user.Username,
		"display_name": user.DisplayName,
		"exp":          time.Now().Add(24 * time.Hour).Unix(),
		"iat":          time.Now().Unix(),
	})

	tokenString, err := token.SignedString([]byte(h.cfg.JWTSecret))
	if err != nil {
		log.Printf("[AUTH] JWT sign error: %v", err)
		writeJSON(w, http.StatusInternalServerError, models.ErrorResponse{Error: "failed to generate token"})
		return
	}

	log.Printf("[AUTH] user logged in: %s", req.Username)
	writeJSON(w, http.StatusOK, models.LoginResponse{
		Token:       tokenString,
		Username:    user.Username,
		DisplayName: user.DisplayName,
	})
}

// GetRooms returns all available chat rooms from PostgreSQL.
// GET /api/rooms
func (h *Handler) GetRooms(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(`SELECT id, name, description, COALESCE(created_by,''), created_at FROM rooms ORDER BY name`)
	if err != nil {
		log.Printf("[AUTH] rooms query error: %v", err)
		writeJSON(w, http.StatusInternalServerError, models.ErrorResponse{Error: "internal error"})
		return
	}
	defer rows.Close()

	var rooms []models.Room
	for rows.Next() {
		var room models.Room
		var createdAt time.Time
		if err := rows.Scan(&room.ID, &room.Name, &room.Description, &room.CreatedBy, &createdAt); err != nil {
			continue
		}
		room.CreatedAt = createdAt.Format(time.RFC3339)
		rooms = append(rooms, room)
	}
	if rooms == nil {
		rooms = []models.Room{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"rooms": rooms})
}

// isPgUniqueViolation checks if a PostgreSQL error is a unique constraint violation.
func isPgUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return contains(err.Error(), "duplicate key") || contains(err.Error(), "23505")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
