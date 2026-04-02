"""
Experiment 2 — Hot-Room vs Multi-Room DynamoDB Partition Test

Two user classes, run separately:

  Pass A (hot room):   locust -f locustfile_exp2.py HotRoomUser --host http://localhost:8080 ...
  Pass B (multi room): locust -f locustfile_exp2.py MultiRoomUser --host http://localhost:8080 ...

Both classes perform identical operations (send messages, send reactions, read messages,
read reactions) at the same rate. The ONLY difference is traffic distribution:

  HotRoomUser:   ALL traffic goes to "room-hot" (single DynamoDB partition key)
  MultiRoomUser: traffic spread evenly across 100 rooms (100 partition keys)

Metrics to compare:
  - POST /api/messages throughput and p50/p95/p99
  - POST /api/reactions throughput and p50/p95/p99
  - GET  /api/messages latency
  - Error rate (DynamoDB throttling surfaces as 500s)
"""

import time
import random
import string
from locust import HttpUser, task, between

REACTION_TYPES = ["like", "love", "laugh", "fire", "surprise", "sad"]
MULTI_ROOMS = [f"room-{i}" for i in range(100)]


def rand_str(n=8):
    return ''.join(random.choices(string.ascii_lowercase + string.digits, k=n))


class _BaseUser(HttpUser):
    """Shared registration/login logic and task definitions."""
    abstract = True
    wait_time = between(0.3, 1.0)

    def _pick_room(self):
        raise NotImplementedError

    def on_start(self):
        self.username = f"exp2_{rand_str(10)}"
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

    def _h(self):
        return {"Authorization": f"Bearer {self.token}"} if self.token else {}

    @task(5)
    def send_message(self):
        """Write to DynamoDB Messages table (PK = room_id)."""
        if not self.token:
            return
        self.client.post("/api/messages", json={
            "room_id": self.room_id,
            "content": f"exp2 {self.username} t={time.time():.3f}",
        }, headers=self._h(), name="POST /api/messages")

    @task(4)
    def send_reaction(self):
        """Write to DynamoDB Reactions table (PK = room_id)."""
        if not self.token:
            return
        self.client.post("/api/reactions", json={
            "room_id": self.room_id,
            "reaction_type": random.choice(REACTION_TYPES),
        }, headers=self._h(), name="POST /api/reactions")

    @task(3)
    def get_messages(self):
        """Read from DynamoDB Messages table."""
        if not self.token:
            return
        since = int((time.time() - 30) * 1000)
        self.client.get(
            f"/api/messages?roomId={self.room_id}&since={since}",
            headers=self._h(), name="GET /api/messages")

    @task(2)
    def get_reactions(self):
        """Read from DynamoDB Reactions table."""
        if not self.token:
            return
        self.client.get(
            f"/api/reactions?roomId={self.room_id}",
            headers=self._h(), name="GET /api/reactions")


class HotRoomUser(_BaseUser):
    """
    ALL traffic to a single room (room-hot).
    Simulates viral scenario: one room goes live, thousands of users pile in.
    All DynamoDB writes hit the SAME partition key → potential throttling.
    """
    def _pick_room(self):
        return "room-hot"


class MultiRoomUser(_BaseUser):
    """
    Traffic spread evenly across 100 rooms.
    Each user picks a random room at startup and sticks with it.
    DynamoDB writes spread across 100 partition keys → no hot partition.
    """
    def _pick_room(self):
        return random.choice(MULTI_ROOMS)
