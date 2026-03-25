"""
LiveChat Load Test — Locust

Usage:
    pip install locust websocket-client
    locust -f locustfile.py --host http://<ALB_DNS>

Experiments:
    1. Scale-Out: vary ECS replicas (1/2/4/8), fixed load
    2. Hot Room: HOT_ROOM_RATIO controls traffic concentration
    3. Sync vs Async: toggle REACTION_MODE env var on backend
    4. WS vs Polling: compare WebSocketUser and PollingUser latency
    5. Cache Hit vs Miss: toggle CACHE_ENABLED env var on backend
    6. Rate Limiting: toggle RATE_LIMIT_RPS env var on backend
"""

import json
import time
import random
import string
from locust import HttpUser, task, between, events
import websocket
import threading

# --- Config ---
HOT_ROOM_RATIO = 0.9        # Experiment 2: 90% traffic to one room
HOT_ROOM_ID = "room-hot"
MULTI_ROOMS = [f"room-{i}" for i in range(100)]
REACTION_TYPES = ["like", "love", "laugh", "fire", "surprise", "sad"]

def rand_str(n=8):
    return ''.join(random.choices(string.ascii_lowercase + string.digits, k=n))


class ChatUser(HttpUser):
    """
    Main simulated user for experiments 1, 2, 3, 5, 6.
    Auto-registers, logs in, sends messages and reactions.
    """
    wait_time = between(0.5, 2)

    def on_start(self):
        self.username = f"user_{rand_str(10)}"
        self.password = "test123456"
        self.token = None
        self.room_id = self._pick_room()

        self.client.post("/api/register", json={
            "username": self.username, "password": self.password,
        })
        resp = self.client.post("/api/login", json={
            "username": self.username, "password": self.password,
        })
        if resp.status_code == 200:
            self.token = resp.json().get("token")

    def _pick_room(self):
        if random.random() < HOT_ROOM_RATIO:
            return HOT_ROOM_ID
        return random.choice(MULTI_ROOMS)

    def _h(self):
        return {"Authorization": f"Bearer {self.token}"} if self.token else {}

    @task(5)
    def send_message(self):
        if not self.token: return
        self.client.post("/api/messages", json={
            "room_id": self.room_id,
            "content": f"msg from {self.username} t={time.time():.3f}",
        }, headers=self._h(), name="/api/messages [POST]")

    @task(3)
    def send_reaction(self):
        if not self.token: return
        self.client.post("/api/reactions", json={
            "room_id": self.room_id,
            "reaction_type": random.choice(REACTION_TYPES),
        }, headers=self._h(), name="/api/reactions [POST]")

    @task(2)
    def get_messages(self):
        if not self.token: return
        since = int((time.time() - 60) * 1000)
        self.client.get(f"/api/messages?roomId={self.room_id}&since={since}",
                        headers=self._h(), name="/api/messages [GET]")

    @task(1)
    def get_reactions(self):
        if not self.token: return
        self.client.get(f"/api/reactions?roomId={self.room_id}",
                        headers=self._h(), name="/api/reactions [GET]")


class PollingUser(HttpUser):
    """
    Experiment 4 baseline — HTTP polling at ~1s interval.
    Measures end-to-end message delivery latency via polling.
    """
    wait_time = between(0.9, 1.1)

    def on_start(self):
        self.username = f"poll_{rand_str(10)}"
        self.password = "test123456"
        self.token = None
        self.room_id = "room-general"
        self.last_ts = int(time.time() * 1000)

        self.client.post("/api/register", json={
            "username": self.username, "password": self.password,
        })
        resp = self.client.post("/api/login", json={
            "username": self.username, "password": self.password,
        })
        if resp.status_code == 200:
            self.token = resp.json().get("token")

    def _h(self):
        return {"Authorization": f"Bearer {self.token}"} if self.token else {}

    @task
    def poll_messages(self):
        if not self.token: return
        resp = self.client.get(
            f"/api/messages?roomId={self.room_id}&since={self.last_ts}",
            headers=self._h(), name="/api/messages [POLL]")
        if resp.status_code == 200:
            data = resp.json()
            now = time.time() * 1000
            for msg in data.get("messages", []):
                ts = msg.get("timestamp", 0)
                if ts > 0:
                    events.request.fire(
                        request_type="POLL_LATENCY",
                        name="e2e_delivery",
                        response_time=now - ts,
                        response_length=0, exception=None, context={})
            msgs = data.get("messages", [])
            if msgs:
                self.last_ts = max(m.get("timestamp", 0) for m in msgs)


class WebSocketUser(HttpUser):
    """
    Experiment 4 improved — WebSocket push.
    Connects via WS and measures push delivery latency.
    """
    wait_time = between(1, 3)

    def on_start(self):
        self.username = f"ws_{rand_str(10)}"
        self.password = "test123456"
        self.token = None
        self.room_id = "room-general"
        self.ws = None

        self.client.post("/api/register", json={
            "username": self.username, "password": self.password,
        })
        resp = self.client.post("/api/login", json={
            "username": self.username, "password": self.password,
        })
        if resp.status_code == 200:
            self.token = resp.json().get("token")
            self._connect()

    def _connect(self):
        if not self.token: return
        host = self.host.replace("http://", "").replace("https://", "")
        url = f"ws://{host}/ws/rooms/{self.room_id}?token={self.token}"
        try:
            self.ws = websocket.WebSocketApp(url,
                on_message=self._on_msg, on_open=lambda ws: None,
                on_error=lambda ws, e: None, on_close=lambda ws, c, m: None)
            t = threading.Thread(target=self.ws.run_forever, daemon=True)
            t.start()
        except: pass

    def _on_msg(self, ws, message):
        try:
            data = json.loads(message)
            if data.get("type") == "chat" and data.get("payload"):
                ts = data["payload"].get("timestamp", 0)
                if ts > 0:
                    events.request.fire(
                        request_type="WS_LATENCY",
                        name="e2e_delivery",
                        response_time=time.time() * 1000 - ts,
                        response_length=len(message), exception=None, context={})
        except: pass

    @task
    def send_message(self):
        if not self.token: return
        self.client.post("/api/messages", json={
            "room_id": self.room_id,
            "content": f"ws_test {self.username} t={time.time():.3f}",
        }, headers={"Authorization": f"Bearer {self.token}"},
        name="/api/messages [WS_USER]")

    def on_stop(self):
        if self.ws: self.ws.close()
