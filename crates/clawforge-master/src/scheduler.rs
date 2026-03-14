use crate::error::MasterError;
use crate::models::{UnitStatus, UnitTask, WorkerInfo};
use crate::pool::WorkerPool;
use chrono::Utc;
use std::collections::VecDeque;
use std::sync::Mutex;
use tracing::info;

/// 分配策略
#[derive(Debug, Clone)]
pub enum ScheduleStrategy {
    RoundRobin,
    LeastLoaded,
    Priority, // 按 CPU 使用率选最空闲
}

/// 任务调度器 (ORCH-009)
///
/// 支持轮询、负载均衡、优先级三种分配策略
pub struct TaskScheduler {
    pool: WorkerPool,
    strategy: ScheduleStrategy,
    rr_index: Mutex<usize>, // round-robin 索引
    pending_queue: Mutex<VecDeque<UnitTask>>,
}

impl TaskScheduler {
    pub fn new(pool: WorkerPool, strategy: ScheduleStrategy) -> Self {
        Self {
            pool,
            strategy,
            rr_index: Mutex::new(0),
            pending_queue: Mutex::new(VecDeque::new()),
        }
    }

    /// 提交单元任务到调度队列
    pub fn enqueue(&self, unit: UnitTask) {
        let mut queue = self.pending_queue.lock().unwrap();
        queue.push_back(unit);
    }

    /// 批量提交
    pub fn enqueue_batch(&self, units: Vec<UnitTask>) {
        let mut queue = self.pending_queue.lock().unwrap();
        for unit in units {
            queue.push_back(unit);
        }
    }

    /// 调度：从队列取出任务分配给 Worker
    pub fn schedule_next(&self) -> Result<Option<(UnitTask, String)>, MasterError> {
        let mut queue = self.pending_queue.lock().unwrap();
        if queue.is_empty() {
            return Ok(None);
        }

        let available = self.pool.get_available();
        if available.is_empty() {
            return Ok(None); // 没有可用 Worker，任务留在队列
        }

        // 选择 Worker
        let worker = self.select_worker(&available)?;

        // 取出任务
        let mut unit = queue.pop_front().unwrap();
        unit.status = UnitStatus::Assigned;
        unit.worker_id = Some(worker.worker_id.clone());
        unit.assigned_at = Some(Utc::now());

        // 标记 Worker 忙碌
        self.pool.mark_busy(&worker.worker_id, &unit.unit_id)?;

        info!(
            unit_id = %unit.unit_id,
            worker_id = %worker.worker_id,
            strategy = ?self.strategy,
            "Unit task assigned"
        );

        Ok(Some((unit, worker.worker_id)))
    }

    /// 批量调度
    pub fn schedule_batch(&self, max: usize) -> Vec<(UnitTask, String)> {
        let mut results = vec![];
        for _ in 0..max {
            match self.schedule_next() {
                Ok(Some(pair)) => results.push(pair),
                _ => break,
            }
        }
        results
    }

    /// 选择 Worker
    fn select_worker(&self, available: &[WorkerInfo]) -> Result<WorkerInfo, MasterError> {
        if available.is_empty() {
            return Err(MasterError::NoAvailableWorkers);
        }

        match self.strategy {
            ScheduleStrategy::RoundRobin => {
                let mut idx = self.rr_index.lock().unwrap();
                let worker = available[*idx % available.len()].clone();
                *idx = (*idx + 1) % available.len();
                Ok(worker)
            }
            ScheduleStrategy::LeastLoaded => {
                let worker = available
                    .iter()
                    .min_by_key(|w| w.active_units)
                    .cloned()
                    .unwrap();
                Ok(worker)
            }
            ScheduleStrategy::Priority => {
                // 按 CPU 使用率选最空闲
                let worker = available
                    .iter()
                    .min_by(|a, b| a.cpu_usage.partial_cmp(&b.cpu_usage).unwrap())
                    .cloned()
                    .unwrap();
                Ok(worker)
            }
        }
    }

    /// 获取队列长度
    pub fn queue_len(&self) -> usize {
        self.pending_queue.lock().unwrap().len()
    }

    /// 获取 Worker 池引用
    pub fn pool(&self) -> &WorkerPool {
        &self.pool
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::models::WorkerStatus;
    use std::collections::HashMap;
    use std::time::Duration;

    fn make_worker(id: &str, active: u32) -> WorkerInfo {
        WorkerInfo {
            worker_id: id.into(),
            address: "127.0.0.1".into(),
            port: 9000,
            status: WorkerStatus::Idle,
            current_unit_id: None,
            cpu_usage: active as f64 * 10.0,
            memory_usage: 0.0,
            disk_usage: 0.0,
            max_concurrent: 5,
            active_units: active,
            last_heartbeat: Utc::now(),
            registered_at: Utc::now(),
            metadata: HashMap::new(),
        }
    }

    fn make_unit(id: &str) -> UnitTask {
        UnitTask {
            unit_id: id.into(),
            subtask_id: "st_1".into(),
            description: "test".into(),
            input: serde_json::json!({}),
            status: UnitStatus::Pending,
            progress: 0,
            worker_id: None,
            output: None,
            error_message: None,
            retry_count: 0,
            max_retries: 3,
            timeout_seconds: 300,
            depends_on: vec![],
            assigned_at: None,
            started_at: None,
            completed_at: None,
            created_at: Utc::now(),
        }
    }

    #[test]
    fn test_least_loaded_scheduling() {
        let pool = WorkerPool::new(10, Duration::from_secs(60));
        pool.register(make_worker("w1", 3)).unwrap();
        pool.register(make_worker("w2", 1)).unwrap();

        let scheduler = TaskScheduler::new(pool, ScheduleStrategy::LeastLoaded);
        scheduler.enqueue(make_unit("u1"));

        let result = scheduler.schedule_next().unwrap().unwrap();
        assert_eq!(result.1, "w2"); // w2 has fewer active units
    }
}
