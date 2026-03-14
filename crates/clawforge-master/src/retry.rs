use crate::error::MasterError;
use crate::models::{ErrorReport, UnitStatus, UnitTask};
use chrono::Utc;
use std::time::Duration;
use tracing::{info, warn};

/// 重试管理器 (ORCH-013)
///
/// 指数退避重试，最多 3 次
pub struct RetryManager {
    base_delay: Duration,
    max_retries: u32,
}

impl RetryManager {
    pub fn new(base_delay: Duration, max_retries: u32) -> Self {
        Self {
            base_delay: if base_delay.is_zero() {
                Duration::from_secs(1)
            } else {
                base_delay
            },
            max_retries: if max_retries == 0 { 3 } else { max_retries },
        }
    }

    /// 判断是否应该重试
    pub fn should_retry(&self, unit: &UnitTask, error: &ErrorReport) -> bool {
        // 不可恢复的错误不重试
        if !error.recoverable {
            return false;
        }

        // 超过最大重试次数
        let max = unit.max_retries.min(self.max_retries);
        if unit.retry_count >= max {
            return false;
        }

        true
    }

    /// 计算重试延迟（指数退避）
    pub fn retry_delay(&self, retry_count: u32) -> Duration {
        // 1s, 2s, 4s, 8s, ...
        let multiplier = 2u64.pow(retry_count);
        let delay = self.base_delay * multiplier as u32;

        // 最大延迟 60 秒
        delay.min(Duration::from_secs(60))
    }

    /// 准备重试：重置单元任务状态
    pub fn prepare_retry(&self, unit: &mut UnitTask) -> Result<Duration, MasterError> {
        let max = unit.max_retries.min(self.max_retries);
        if unit.retry_count >= max {
            return Err(MasterError::RetryExhausted(
                unit.unit_id.clone(),
                max,
            ));
        }

        let delay = self.retry_delay(unit.retry_count);

        unit.retry_count += 1;
        unit.status = UnitStatus::Retrying;
        unit.worker_id = None;
        unit.assigned_at = None;
        unit.started_at = None;
        unit.completed_at = None;
        unit.error_message = None;
        unit.progress = 0;

        info!(
            unit_id = %unit.unit_id,
            retry_count = unit.retry_count,
            delay_ms = delay.as_millis(),
            "Unit task prepared for retry"
        );

        Ok(delay)
    }

    /// 处理错误报告，决定是否重试
    pub fn handle_error(
        &self,
        unit: &mut UnitTask,
        error: &ErrorReport,
    ) -> RetryDecision {
        if !self.should_retry(unit, error) {
            // 不重试，标记为最终失败
            unit.status = UnitStatus::Failed;
            unit.error_message = Some(error.error_message.clone());
            unit.completed_at = Some(Utc::now());

            warn!(
                unit_id = %unit.unit_id,
                retry_count = unit.retry_count,
                error = %error.error_message,
                recoverable = error.recoverable,
                "Unit task failed permanently"
            );

            return RetryDecision::GiveUp {
                reason: if !error.recoverable {
                    "non-recoverable error".into()
                } else {
                    format!("max retries ({}) exhausted", unit.max_retries)
                },
            };
        }

        match self.prepare_retry(unit) {
            Ok(delay) => RetryDecision::Retry { delay },
            Err(e) => RetryDecision::GiveUp {
                reason: e.to_string(),
            },
        }
    }
}

/// 重试决策
#[derive(Debug)]
pub enum RetryDecision {
    Retry { delay: Duration },
    GiveUp { reason: String },
}

impl Default for RetryManager {
    fn default() -> Self {
        Self::new(Duration::from_secs(1), 3)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_unit(retries: u32) -> UnitTask {
        UnitTask {
            unit_id: "u1".into(),
            subtask_id: "st_1".into(),
            description: "test".into(),
            input: serde_json::json!({}),
            status: UnitStatus::Failed,
            progress: 0,
            worker_id: Some("w1".into()),
            output: None,
            error_message: Some("test error".into()),
            retry_count: retries,
            max_retries: 3,
            timeout_seconds: 300,
            depends_on: vec![],
            assigned_at: None,
            started_at: None,
            completed_at: None,
            created_at: Utc::now(),
        }
    }

    fn make_error(recoverable: bool) -> ErrorReport {
        ErrorReport {
            unit_id: "u1".into(),
            subtask_id: "st_1".into(),
            worker_id: "w1".into(),
            error_code: "ERR_001".into(),
            error_message: "something failed".into(),
            stack_trace: None,
            recoverable,
            retry_count: 0,
            timestamp: Utc::now(),
        }
    }

    #[test]
    fn test_exponential_backoff() {
        let mgr = RetryManager::new(Duration::from_secs(1), 3);
        assert_eq!(mgr.retry_delay(0), Duration::from_secs(1));
        assert_eq!(mgr.retry_delay(1), Duration::from_secs(2));
        assert_eq!(mgr.retry_delay(2), Duration::from_secs(4));
    }

    #[test]
    fn test_retry_decision() {
        let mgr = RetryManager::default();
        let mut unit = make_unit(0);
        let error = make_error(true);

        match mgr.handle_error(&mut unit, &error) {
            RetryDecision::Retry { delay } => {
                assert_eq!(delay, Duration::from_secs(1));
                assert_eq!(unit.retry_count, 1);
                assert_eq!(unit.status, UnitStatus::Retrying);
            }
            _ => panic!("expected retry"),
        }
    }

    #[test]
    fn test_no_retry_non_recoverable() {
        let mgr = RetryManager::default();
        let mut unit = make_unit(0);
        let error = make_error(false);

        match mgr.handle_error(&mut unit, &error) {
            RetryDecision::GiveUp { reason } => {
                assert!(reason.contains("non-recoverable"));
            }
            _ => panic!("expected give up"),
        }
    }

    #[test]
    fn test_no_retry_exhausted() {
        let mgr = RetryManager::default();
        let mut unit = make_unit(3); // already at max
        let error = make_error(true);

        match mgr.handle_error(&mut unit, &error) {
            RetryDecision::GiveUp { reason } => {
                assert!(reason.contains("max retries"));
            }
            _ => panic!("expected give up"),
        }
    }
}
