package media

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"livechat/internal/config"
	"livechat/internal/middleware"
	"livechat/internal/models"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
)

// Handler manages file uploads to S3.
// Design: large blobs stay in S3; DynamoDB stores only the reference URL.
// This is the standard hot-path vs cold-path data separation pattern.
type Handler struct {
	s3  *s3.Client
	cfg *config.Config
}

func NewHandler(s3Client *s3.Client, cfg *config.Config) *Handler {
	return &Handler{s3: s3Client, cfg: cfg}
}

// Upload handles POST /api/upload (multipart/form-data).
// Returns the S3 URL for the uploaded file.
func (h *Handler) Upload(w http.ResponseWriter, r *http.Request) {
	username := middleware.GetUsername(r)
	r.ParseMultipartForm(10 << 20) // 10 MB limit

	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, models.ErrorResponse{Error: "file field required"})
		return
	}
	defer file.Close()

	ext := filepath.Ext(header.Filename)
	key := fmt.Sprintf("uploads/%s/%s%s", username, uuid.New().String(), ext)

	_, err = h.s3.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket:      aws.String(h.cfg.S3Bucket),
		Key:         aws.String(key),
		Body:        file,
		ContentType: aws.String(header.Header.Get("Content-Type")),
	})
	if err != nil {
		log.Printf("[MEDIA] S3 upload error: %v", err)
		writeJSON(w, http.StatusInternalServerError, models.ErrorResponse{Error: "upload failed"})
		return
	}

	url := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", h.cfg.S3Bucket, h.cfg.AWSRegion, key)
	log.Printf("[MEDIA] uploaded: user=%s key=%s size=%d", username, key, header.Size)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"url":         url,
		"key":         key,
		"filename":    header.Filename,
		"size":        header.Size,
		"uploaded_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
