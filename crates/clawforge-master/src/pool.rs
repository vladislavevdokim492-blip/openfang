use crate::error::MasterError;
use crate::models::{WorkerInfo, WorkerStatus};
use chrono::Utc;
use dashmap::DashMap;
use std::sync::Arc;
use std::time::Duration;
use tracing::{info, warn};

/// Worker 池管理器 (ORCH-008)
///
/// 管理 Worker 注册、心跳、状态，支持动态添加/移除
pub struct WorkerPool {
    workers: Arc<DashMap<String, WorkerInfo>>,
    max_workers: usize,
    heartbeat_timeout: Duration,
}

impl WorkerPool {
    pub fn new(max_workers: usize, heartbeat_timeout: Duration) -> Self {
        Self {
            workers: Arc::new(DashMap::new()),
            max_workers: if max_workers == 0 { 100 } else { max_workers },
            heartbeat_timeout: if heartbeat_timeout.is_zero() {
                Duration::from_secs(60)
            } else {
                heartbeat_timeout
            },
        }
    }

    /// 注册 Worker
    pub fn register(&self, mut worker: WorkerInfo) -> Result<(), MasterError> {
        if self.workers.len() >= self.max_workers {
            return Err(MasterError::PoolFull);
        }

        let id = worker.worker_id.clone();
        worker.registered_at = Utc::now();
        worker.last_heartbeat = Utc::now();
        worker.status = WorkerStatus::Idle;

        self.workers.insert(id.clone(), worker);
        info!(worker_id = %id, "Worker registered");
        Ok(())
    }

    /// 注销 Worker
    pub fn deregister(&self, worker_id: &str) -> Result<(), MasterError> {
        self.workers
            .remove(worker_id)
            .ok_or_else(|| MasterError::WorkerNotFound(worker_id.into()))?;
        info!(worker_id = %worker_id, "Worker deregistered");
        Ok(())
    }

    /// 更新心跳
    pub fn heartbeat(
        &self,
        worker_id: &str,
        cpu: f64,
        memory: f64,
        disk: f64,
        active_units: u32,
    ) -> Result<(), MasterError> {
        let mut entry = self
            .workers
            .get_mut(worker_id)
            .ok_or_else(|| MasterError::WorkerNotFound(worker_id.into()))?;

        let worker = entry.value_mut();
        worker.last_heartbeat = Utc::now();
        worker.cpu_usage = cpu;
        worker.memory_usage = memory;
        worker.disk_usage = disk;
        worker.active_units = active_units;

        // 自动更新状态
        if active_units >= worker.max_concurrent {
            worker.status = WorkerStatus::Busy;
        } else if worker.status == WorkerStatus::Busy {
            worker.status = WorkerStatus::Idle;
        }

        Ok(())
    }

    /// 获取可用 Worker（空闲且心跳正常）
    pub fn get_available(&self) -> Vec<WorkerInfo> {
        let cutoff = Utc::now() - chrono::Duration::from_std(self.heartbeat_timeout).unwrap();

        self.workers
            .iter()
            .filter(|entry| {
                let w = entry.value();
                w.status == WorkerStatus::Idle
                    && w.last_heartbeat > cutoff
                    && w.active_units < w.max_concurrent
            })
            .map(|entry| entry.value().clone())
            .collect()
    }

    /// 获取所有 Worker
    pub fn get_all(&self) -> Vec<WorkerInfo> {
        self.workers.iter().map(|e| e.value().clone()).collect()
    }

    /// 获取单个 Worker
    pub fn get(&self, worker_id: &str) -> Result<WorkerInfo, MasterError> {
        self.workers
            .get(worker_id)
            .map(|e| e.value().clone())
            .ok_or_else(|| MasterError::WorkerNotFound(worker_id.into()))
    }

    /// 标记 Worker 为忙碌
    pub fn mark_busy(&self, worker_id: &str, unit_id: &str) -> Result<(), MasterError> {
        let mut entry = self
            .workers
            .get_mut(worker_id)
            .ok_or_else(|| MasterError::WorkerNotFound(worker_id.into()))?;

        let worker = entry.value_mut();
        worker.active_units += 1;
        worker.current_unit_id = Some(unit_id.into());
        if worker.active_units >= worker.max_concurrent {
            worker.status = WorkerStatus::Busy;
        }
        Ok(())
    }

    /// 标记 Worker 任务完成
    pub fn mark_unit_done(&self, worker_id: &str) -> Result<(), MasterError> {
        let mut entry = self
            .workers
            .get_mut(worker_id)
            .ok_or_else(|| MasterError::WorkerNotFound(worker_id.into()))?;

        let worker = entry.value_mut();
        worker.active_units = worker.active_units.saturating_sub(1);
        if worker.active_units == 0 {
            worker.current_unit_id = None;
        }
        if worker.status == WorkerStatus::Busy && worker.active_units < worker.max_concurrent {
            worker.status = WorkerStatus::Idle;
        }
        Ok(())
    }

    /// 清理超时 Worker
    pub fn cleanup_stale(&self) -> Vec<String> {
        let cutoff = Utc::now() - chrono::Duration::from_std(self.heartbeat_timeout * 2).unwrap();
        let mut removed = vec![];

        self.workers.retain(|id, worker| {
            if worker.last_heartbeat < cutoff {
                warn!(worker_id = %id, "Removing stale worker");
                removed.push(id.clone());
                false
            } else {
                true
            }
        });

        removed
    }

    /// Worker 数量
    pub fn len(&self) -> usize {
        self.workers.len()
    }

    /// 是否为空
    pub fn is_empty(&self) -> bool {
        self.workers.is_empty()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::HashMap;

    fn make_worker(id: &str) -> WorkerInfo {
        WorkerInfo {
            worker_id: id.into(),
            address: "127.0.0.1".into(),
            port: 9000,
            status: WorkerStatus::Idle,
            current_unit_id: None,
            cpu_usage: 0.0,
            memory_usage: 0.0,
            disk_usage: 0.0,
            max_concurrent: 5,
            active_units: 0,
            last_heartbeat: Utc::now(),
            registered_at: Utc::now(),
            metadata: HashMap::new(),
        }
    }

    #[test]
    fn test_register_and_get() {
        let pool = WorkerPool::new(10, Duration::from_secs(60));
        pool.register(make_worker("w1")).unwrap();
        assert_eq!(pool.len(), 1);
        let w = pool.get("w1").unwrap();
        assert_eq!(w.worker_id, "w1");
    }

    #[test]
    fn test_pool_full() {
        let pool = WorkerPool::new(1, Duration::from_secs(60));
        pool.register(make_worker("w1")).unwrap();
        assert!(pool.register(make_worker("w2")).is_err());
    }

    #[test]
    fn test_mark_busy_and_done() {
        let pool = WorkerPool::new(10, Duration::from_secs(60));
        pool.register(make_worker("w1")).unwrap();
        pool.mark_busy("w1", "unit_1").unwrap();
        let w = pool.get("w1").unwrap();
        assert_eq!(w.active_units, 1);

        pool.mark_unit_done("w1").unwrap();
        let w = pool.get("w1").unwrap();
        assert_eq!(w.active_units, 0);
    }
}
