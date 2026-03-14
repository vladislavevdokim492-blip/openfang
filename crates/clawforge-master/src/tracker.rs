use crate::error::MasterError;
use crate::models::{AggregatedResult, ProgressReport, ResultReport, UnitStatus, UnitTask};
use chrono::Utc;
use dashmap::DashMap;
use std::sync::Arc;
use tracing::info;

/// 进度跟踪器 (ORCH-010)
///
/// 实时监控子任务下所有单元任务的进度
pub struct ProgressTracker {
    /// subtask_id -> Vec<UnitTask>
    units: Arc<DashMap<String, Vec<UnitTask>>>,
}

impl ProgressTracker {
    pub fn new() -> Self {
        Self {
            units: Arc::new(DashMap::new()),
        }
    }

    /// 注册单元任务
    pub fn register_units(&self, subtask_id: &str, units: Vec<UnitTask>) {
        self.units.insert(subtask_id.into(), units);
    }

    /// 更新单元任务进度
    pub fn update_progress(&self, report: &ProgressReport) -> Result<(), MasterError> {
        let mut entry = self
            .units
            .get_mut(&report.subtask_id)
            .ok_or_else(|| MasterError::TaskNotFound(report.subtask_id.clone()))?;

        let units = entry.value_mut();
        let unit = units
            .iter_mut()
            .find(|u| u.unit_id == report.unit_id)
            .ok_or_else(|| MasterError::UnitNotFound(report.unit_id.clone()))?;

        unit.progress = report.progress;
        if unit.status == UnitStatus::Assigned {
            unit.status = UnitStatus::Running;
            unit.started_at = Some(Utc::now());
        }

        Ok(())
    }

    /// 标记单元任务完成
    pub fn mark_completed(&self, report: &ResultReport) -> Result<(), MasterError> {
        let mut entry = self
            .units
            .get_mut(&report.subtask_id)
            .ok_or_else(|| MasterError::TaskNotFound(report.subtask_id.clone()))?;

        let units = entry.value_mut();
        let unit = units
            .iter_mut()
            .find(|u| u.unit_id == report.unit_id)
            .ok_or_else(|| MasterError::UnitNotFound(report.unit_id.clone()))?;

        unit.status = report.status.clone();
        unit.output = report.output.clone();
        unit.progress = 100;
        unit.completed_at = Some(report.completed_at);

        Ok(())
    }

    /// 标记单元任务失败
    pub fn mark_failed(&self, unit_id: &str, subtask_id: &str, error: &str) -> Result<(), MasterError> {
        let mut entry = self
            .units
            .get_mut(subtask_id)
            .ok_or_else(|| MasterError::TaskNotFound(subtask_id.into()))?;

        let units = entry.value_mut();
        let unit = units
            .iter_mut()
            .find(|u| u.unit_id == unit_id)
            .ok_or_else(|| MasterError::UnitNotFound(unit_id.into()))?;

        unit.status = UnitStatus::Failed;
        unit.error_message = Some(error.into());
        unit.completed_at = Some(Utc::now());

        Ok(())
    }

    /// 获取子任务总进度 (0-100)
    pub fn get_subtask_progress(&self, subtask_id: &str) -> Result<u32, MasterError> {
        let entry = self
            .units
            .get(subtask_id)
            .ok_or_else(|| MasterError::TaskNotFound(subtask_id.into()))?;

        let units = entry.value();
        if units.is_empty() {
            return Ok(0);
        }

        let total: u32 = units.iter().map(|u| u.progress).sum();
        Ok(total / units.len() as u32)
    }

    /// 检查子任务是否全部完成
    pub fn is_subtask_done(&self, subtask_id: &str) -> Result<bool, MasterError> {
        let entry = self
            .units
            .get(subtask_id)
            .ok_or_else(|| MasterError::TaskNotFound(subtask_id.into()))?;

        Ok(entry.value().iter().all(|u| {
            matches!(
                u.status,
                UnitStatus::Completed | UnitStatus::Failed | UnitStatus::Cancelled
            )
        }))
    }

    /// 汇总子任务结果 (ORCH-011)
    pub fn aggregate_results(&self, subtask_id: &str) -> Result<AggregatedResult, MasterError> {
        let entry = self
            .units
            .get(subtask_id)
            .ok_or_else(|| MasterError::TaskNotFound(subtask_id.into()))?;

        let units = entry.value();
        let total = units.len();
        let completed = units.iter().filter(|u| u.status == UnitStatus::Completed).count();
        let failed = units.iter().filter(|u| u.status == UnitStatus::Failed).count();

        // 合并输出
        let outputs: Vec<serde_json::Value> = units
            .iter()
            .filter_map(|u| {
                if u.status == UnitStatus::Completed {
                    u.output.clone()
                } else {
                    None
                }
            })
            .collect();

        let merged = serde_json::json!({
            "unit_outputs": outputs,
            "total_units": total,
            "completed": completed,
            "failed": failed,
        });

        // 计算质量分数
        let quality = if total > 0 {
            (completed as f64 / total as f64) * 100.0
        } else {
            0.0
        };

        // 计算耗时
        let duration_ms = units
            .iter()
            .filter_map(|u| {
                if let (Some(start), Some(end)) = (&u.started_at, &u.completed_at) {
                    Some((*end - *start).num_milliseconds() as u64)
                } else {
                    None
                }
            })
            .max()
            .unwrap_or(0);

        info!(
            subtask_id = %subtask_id,
            total = total,
            completed = completed,
            failed = failed,
            quality = quality,
            "Results aggregated"
        );

        Ok(AggregatedResult {
            subtask_id: subtask_id.into(),
            total_units: total,
            completed,
            failed,
            merged_output: merged,
            quality_score: quality,
            duration_ms,
        })
    }

    /// 获取子任务的所有单元任务
    pub fn get_units(&self, subtask_id: &str) -> Result<Vec<UnitTask>, MasterError> {
        let entry = self
            .units
            .get(subtask_id)
            .ok_or_else(|| MasterError::TaskNotFound(subtask_id.into()))?;
        Ok(entry.value().clone())
    }

    /// 清理已完成的子任务数据
    pub fn cleanup(&self, subtask_id: &str) {
        self.units.remove(subtask_id);
    }
}

impl Default for ProgressTracker {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::models::TaskPriority;

    fn make_unit(id: &str, subtask_id: &str) -> UnitTask {
        UnitTask {
            unit_id: id.into(),
            subtask_id: subtask_id.into(),
            description: "test".into(),
            input: serde_json::json!({}),
            status: UnitStatus::Assigned,
            progress: 0,
            worker_id: Some("w1".into()),
            output: None,
            error_message: None,
            retry_count: 0,
            max_retries: 3,
            timeout_seconds: 300,
            depends_on: vec![],
            assigned_at: Some(Utc::now()),
            started_at: None,
            completed_at: None,
            created_at: Utc::now(),
        }
    }

    #[test]
    fn test_progress_tracking() {
        let tracker = ProgressTracker::new();
        tracker.register_units("st_1", vec![make_unit("u1", "st_1"), make_unit("u2", "st_1")]);

        // Update progress
        tracker
            .update_progress(&ProgressReport {
                unit_id: "u1".into(),
                subtask_id: "st_1".into(),
                worker_id: "w1".into(),
                progress: 50,
                message: "halfway".into(),
                timestamp: Utc::now(),
            })
            .unwrap();

        assert_eq!(tracker.get_subtask_progress("st_1").unwrap(), 25); // (50+0)/2
    }

    #[test]
    fn test_aggregate_results() {
        let tracker = ProgressTracker::new();
        let mut u1 = make_unit("u1", "st_1");
        u1.status = UnitStatus::Completed;
        u1.output = Some(serde_json::json!({"result": "ok"}));
        u1.started_at = Some(Utc::now());
        u1.completed_at = Some(Utc::now());

        tracker.register_units("st_1", vec![u1]);

        let result = tracker.aggregate_results("st_1").unwrap();
        assert_eq!(result.completed, 1);
        assert_eq!(result.failed, 0);
        assert_eq!(result.quality_score, 100.0);
    }
}
