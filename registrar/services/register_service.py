"""
Register Service - Registration task management

This service manages registration tasks and provides HTTP API for Klein frontend.
"""

from __future__ import annotations

import json
import threading
import time
import uuid
from concurrent.futures import FIRST_COMPLETED, ThreadPoolExecutor, wait
from concurrent.futures import CancelledError
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from services import openai_register
from services import config as register_config


# === Configuration file ===
DATA_DIR = Path(__file__).resolve().parent.parent / "data"
REGISTER_FILE = DATA_DIR / "register.json"


def _now() -> str:
    return datetime.now(timezone.utc).isoformat()


def _default_config() -> dict:
    return {
        **openai_register.config,
        "mode": "total",
        "target_quota": 100,
        "target_available": 10,
        "check_interval": 5,
        "enabled": False,
        "stats": {
            "success": 0,
            "fail": 0,
            "done": 0,
            "running": 0,
            "threads": openai_register.config["threads"],
            "elapsed_seconds": 0,
            "avg_seconds": 0,
            "success_rate": 0,
            "current_quota": 0,
            "current_available": 0,
        },
        "logs": [],
    }


def _normalize(raw: dict) -> dict:
    cfg = _default_config()
    cfg.update({k: v for k, v in raw.items() if k not in {"stats", "logs"}})
    cfg["total"] = max(1, int(cfg.get("total") or 1))
    cfg["threads"] = max(1, int(cfg.get("threads") or 1))
    cfg["mode"] = str(cfg.get("mode") or "total").strip() if str(cfg.get("mode") or "total").strip() in {"total", "quota", "available"} else "total"
    cfg["target_quota"] = max(1, int(cfg.get("target_quota") or 1))
    cfg["target_available"] = max(1, int(cfg.get("target_available") or 1))
    cfg["check_interval"] = max(1, int(cfg.get("check_interval") or 5))
    cfg["proxy"] = str(cfg.get("proxy") or "").strip()
    cfg["flaresolverr_url"] = str(cfg.get("flaresolverr_url") or "").strip()
    cfg["klein_api_base"] = str(cfg.get("klein_api_base") or "").strip()
    cfg["klein_jwt"] = str(cfg.get("klein_jwt") or "").strip()
    cfg["enabled"] = bool(cfg.get("enabled"))
    stats = {**_default_config()["stats"], **(raw.get("stats") if isinstance(raw.get("stats"), dict) else {}), "threads": cfg["threads"]}
    cfg["stats"] = stats
    logs = raw.get("logs") if isinstance(raw.get("logs"), list) else []
    cfg["logs"] = logs[-80:] if logs else []
    return cfg


class RegisterService:
    def __init__(self, store_file: Path):
        self._store_file = store_file
        self._lock = threading.RLock()
        self._runner: threading.Thread | None = None
        self._logs: list[dict] = []
        self._subscribers: list[Any] = []
        openai_register.register_log_sink = self._append_log
        self._config = self._load()
        if self._config["enabled"]:
            self.start()

    def _load(self) -> dict:
        try:
            if self._store_file.exists():
                return _normalize(json.loads(self._store_file.read_text(encoding="utf-8")))
        except Exception:
            pass
        return _normalize({})

    def _save(self) -> None:
        self._store_file.parent.mkdir(parents=True, exist_ok=True)
        self._store_file.write_text(json.dumps(self._config, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")

    def get(self) -> dict:
        with self._lock:
            cfg = json.loads(json.dumps({**self._config, "logs": self._logs[-80:]}, ensure_ascii=False))
            return cfg

    def _inject_proxy_to_mail(self) -> None:
        proxy = str(self._config.get("proxy") or "").strip()
        if proxy and isinstance(self._config.get("mail"), dict):
            self._config["mail"]["proxy"] = proxy

    def update(self, updates: dict) -> dict:
        with self._lock:
            # Update config
            if updates.get("mail") and isinstance(updates["mail"], dict):
                self._config["mail"] = updates["mail"]
            if updates.get("proxy"):
                self._config["proxy"] = updates["proxy"]
            if updates.get("flaresolverr_url"):
                self._config["flaresolverr_url"] = updates["flaresolverr_url"]
            if updates.get("klein_api_base"):
                self._config["klein_api_base"] = updates["klein_api_base"]
            if updates.get("klein_jwt"):
                self._config["klein_jwt"] = updates["klein_jwt"]
            if updates.get("klein_refresh_token"):
                self._config["klein_refresh_token"] = updates["klein_refresh_token"]
            if updates.get("total"):
                self._config["total"] = max(1, int(updates["total"]))
            if updates.get("threads"):
                self._config["threads"] = max(1, int(updates["threads"]))
            if updates.get("mode"):
                mode = str(updates["mode"]).strip()
                if mode in {"total", "quota", "available"}:
                    self._config["mode"] = mode
            if updates.get("target_quota"):
                self._config["target_quota"] = max(1, int(updates["target_quota"]))
            if updates.get("target_available"):
                self._config["target_available"] = max(1, int(updates["target_available"]))
            if updates.get("check_interval"):
                self._config["check_interval"] = max(1, int(updates["check_interval"]))

            self._inject_proxy_to_mail()
            self._sync_to_register_config()
            self._save()
            return self.get()

    def _sync_to_register_config(self) -> None:
        """Sync config to openai_register module"""
        openai_register.config["mail"] = self._config["mail"]
        openai_register.config["proxy"] = self._config["proxy"]
        openai_register.config["flaresolverr_url"] = self._config["flaresolverr_url"]
        openai_register.config["klein_api_base"] = self._config["klein_api_base"]
        openai_register.config["klein_jwt"] = self._config["klein_jwt"]
        openai_register.config["klein_refresh_token"] = self._config.get("klein_refresh_token", "")
        openai_register.config["total"] = self._config["total"]
        openai_register.config["threads"] = self._config["threads"]

    def start(self) -> dict:
        with self._lock:
            if self._runner and self._runner.is_alive():
                self._append_log("注册任务仍在停止中，请等待当前批次退出后再启动", "yellow")
                return self.get()
            openai_register.stop_event.clear()
            self._config["enabled"] = True
            self._inject_proxy_to_mail()
            self._logs = []
            self._config["stats"] = {
                "job_id": uuid.uuid4().hex,
                "success": 0,
                "fail": 0,
                "done": 0,
                "running": 0,
                "threads": self._config["threads"],
                "started_at": _now(),
                "updated_at": _now(),
                "elapsed_seconds": 0,
                "avg_seconds": 0,
                "success_rate": 0,
                "current_quota": 0,
                "current_available": 0,
            }
            self._sync_to_register_config()
            with openai_register.stats_lock:
                openai_register.stats.update({"done": 0, "success": 0, "fail": 0, "start_time": time.time()})
            self._save()
            self._runner = threading.Thread(target=self._run, daemon=True, name="openai-register")
            self._runner.start()
            self._append_log(f"注册任务启动，模式={self._config['mode']}，线程数={self._config['threads']}", "yellow")
            return self.get()

    def stop(self) -> dict:
        with self._lock:
            openai_register.stop_event.set()
            self._config["enabled"] = False
            self._config["stats"]["updated_at"] = _now()
            self._save()
            running = int((self._config.get("stats") or {}).get("running") or 0)
            if running > 0:
                self._append_log("已请求停止注册任务，正在中断当前运行任务", "yellow")
            else:
                self._append_log("注册任务已停止", "yellow")
            return self.get()

    def reset(self) -> dict:
        with self._lock:
            openai_register.stop_event.set()
            self._logs = []
            self._config["stats"] = {
                "success": 0,
                "fail": 0,
                "done": 0,
                "running": 0,
                "threads": self._config["threads"],
                "elapsed_seconds": 0,
                "avg_seconds": 0,
                "success_rate": 0,
                "current_quota": 0,
                "current_available": 0,
                "updated_at": _now(),
            }
            with openai_register.stats_lock:
                openai_register.stats.update({"done": 0, "success": 0, "fail": 0, "start_time": 0.0})
            self._save()
            return self.get()

    def _append_log(self, text: str, color: str = "") -> None:
        with self._lock:
            self._logs.append({"time": _now(), "text": str(text), "level": str(color or "info")})
            self._logs = self._logs[-80:]
            self._notify_subscribers()

    def _notify_subscribers(self) -> None:
        cfg = self.get()
        for sub in self._subscribers:
            try:
                sub(cfg)
            except Exception:
                pass

    def subscribe(self, callback: Any) -> None:
        with self._lock:
            self._subscribers.append(callback)

    def unsubscribe(self, callback: Any) -> None:
        with self._lock:
            if callback in self._subscribers:
                self._subscribers.remove(callback)

    def _pool_metrics(self) -> dict:
        """Get pool metrics from Klein API"""
        if not self._config.get("klein_api_base"):
            return {"current_quota": 0, "current_available": 0}
        if not openai_register.ensure_valid_klein_jwt():
            self._sync_from_openai_register_config()
            self._save()
            self._append_log("Klein JWT 不可用，无法读取号池统计", "yellow")
            return {"current_quota": 0, "current_available": 0}

        try:
            import requests
            resp = requests.get(
                f"{self._config['klein_api_base']}/admin/api/v1/accounts/stats",
                headers={
                    "Authorization": f"Bearer {openai_register.config['klein_jwt']}",
                },
                timeout=10,
                verify=False,
            )
            if resp.status_code == 401 and openai_register.refresh_klein_jwt():
                self._sync_from_openai_register_config()
                self._save()
                resp = requests.get(
                    f"{self._config['klein_api_base']}/admin/api/v1/accounts/stats",
                    headers={
                        "Authorization": f"Bearer {openai_register.config['klein_jwt']}",
                    },
                    timeout=10,
                    verify=False,
                )
            if resp.status_code == 200:
                data = resp.json().get("data", {})
                if isinstance(data.get("providers"), list):
                    providers = [item for item in data["providers"] if isinstance(item, dict)]
                    gpt = next((item for item in providers if str(item.get("provider") or "").lower() == "gpt"), None)
                    target = gpt or data
                    return {
                        "current_quota": int(target.get("total_quota") or target.get("quota_remaining") or 0),
                        "current_available": int(target.get("available") or 0),
                    }
                if isinstance(data.get("pool"), dict):
                    return {
                        "current_quota": int(data.get("total_quota") or data.get("quota_remaining") or 0),
                        "current_available": int(data.get("available") or data["pool"].get("gpt") or 0),
                    }
                return {
                    "current_quota": int(data.get("total_quota") or data.get("quota_remaining") or 0),
                    "current_available": int(data.get("available") or 0),
                }
            self._append_log(f"读取号池统计失败: HTTP {resp.status_code}", "yellow")
        except Exception:
            pass
        return {"current_quota": 0, "current_available": 0}

    def _sync_from_openai_register_config(self) -> None:
        self._config["klein_jwt"] = openai_register.config.get("klein_jwt", "")
        self._config["klein_refresh_token"] = openai_register.config.get("klein_refresh_token", "")
        self._config["klein_jwt_expires_at"] = openai_register.config.get("klein_jwt_expires_at", 0)

    def _target_reached(self, cfg: dict, submitted: int) -> bool:
        mode = str(cfg.get("mode") or "total")
        metrics = self._pool_metrics()
        self._bump(**metrics)
        if mode == "quota":
            reached = metrics["current_quota"] >= int(cfg.get("target_quota") or 1)
            self._append_log(f"检查号池：当前额度={metrics['current_quota']}，目标额度={cfg.get('target_quota')}，{'跳过注册' if reached else '继续注册'}", "yellow")
            return reached
        if mode == "available":
            reached = metrics["current_available"] >= int(cfg.get("target_available") or 1)
            self._append_log(f"检查号池：当前账号={metrics['current_available']}，目标账号={cfg.get('target_available')}，{'跳过注册' if reached else '继续注册'}", "yellow")
            return reached
        return submitted >= int(cfg.get("total") or 1)

    def _bump(self, **updates) -> None:
        with self._lock:
            self._config["stats"].update(updates)
            stats = self._config["stats"]
            started_at = str(stats.get("started_at") or "")
            if started_at:
                try:
                    elapsed = max(0.0, (datetime.now(timezone.utc) - datetime.fromisoformat(started_at)).total_seconds())
                except Exception:
                    elapsed = 0.0
                done = int(stats.get("done") or 0)
                success = int(stats.get("success") or 0)
                fail = int(stats.get("fail") or 0)
                stats["elapsed_seconds"] = round(elapsed, 1)
                stats["avg_seconds"] = round(elapsed / success, 1) if success else 0
                stats["success_rate"] = round(success * 100 / max(1, success + fail), 1)
            self._config["stats"]["updated_at"] = _now()
            self._save()
            self._notify_subscribers()

    def _run(self) -> None:
        threads = int(self.get()["threads"])
        submitted, done, success, fail = 0, 0, 0, 0
        with ThreadPoolExecutor(max_workers=threads) as executor:
            futures = set()
            while True:
                cfg = self.get()
                if not cfg.get("enabled"):
                    for future in list(futures):
                        future.cancel()
                    if not futures:
                        break
                while (
                    cfg.get("enabled")
                    and not openai_register.stop_event.is_set()
                    and len(futures) < threads
                    and not self._target_reached(cfg, submitted)
                ):
                    submitted += 1
                    futures.add(executor.submit(openai_register.worker, submitted))
                self._bump(running=len(futures), done=done, success=success, fail=fail)
                if not futures and (not self.get()["enabled"] or str(cfg.get("mode") or "total") == "total"):
                    break
                if not futures:
                    if openai_register.stop_event.wait(max(1, int(cfg.get("check_interval") or 5))):
                        break
                    continue
                finished, futures = wait(futures, timeout=1, return_when=FIRST_COMPLETED)
                if not finished:
                    continue
                for future in finished:
                    try:
                        result = future.result()
                        if result.get("stopped"):
                            continue
                        done += 1
                        success += 1 if result.get("ok") else 0
                        fail += 0 if result.get("ok") else 1
                    except CancelledError:
                        continue
                    except Exception:
                        done += 1
                        fail += 1
        self._bump(running=0, done=done, success=success, fail=fail, finished_at=_now())
        with self._lock:
            self._config["enabled"] = False
            self._save()
        self._append_log(f"注册任务结束，成功{success}，失败{fail}", "yellow")


# Global service instance
register_service = RegisterService(REGISTER_FILE)
