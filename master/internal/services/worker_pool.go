package services

import (
	"fmt"
	"log"
	"math/rand"
	"sort"
	"sync"
	"time"

	"github.com/clawforge/p3-go/claw/master/internal/models"
)

// WorkerPoolService Worker 池管理服务 (ORCH-008 + ORCH-009)
type WorkerPoolService struct {
	mu       sync.RWMutex
	workers  map[string]*models.WorkerNode
	config   WorkerPoolConfig
	strategy AssignmentStrategy
}

// WorkerPoolConfig Worker 池配置
type WorkerPoolConfig struct {
	HeartbeatTimeout    time.Duration // 心跳超时（默认 30s）
	MaxWorkersPerPool   int           // 最大 Worker 数
	AssignmentStrategy  string        // round_robin, least_loaded, priority, random
	HealthCheckInterval time.Duration // 健康检查间隔
}

// AssignmentStrategy 分配策略接口
type AssignmentStrategy interface {
	Select(workers []*models.WorkerNode) *models.WorkerNode
	Name() string
}

// NewWorkerPoolService 创建 Worker 池服务
func NewWorkerPoolService(cfg WorkerPoolConfig) *WorkerPoolService {
	if cfg.HeartbeatTimeout == 0 {
		cfg.HeartbeatTimeout = 30 * time.Second
	}
	if cfg.MaxWorkersPerPool == 0 {
		cfg.MaxWorkersPerPool = 100
	}
	if cfg.HealthCheckInterval == 0 {
		cfg.HealthCheckInterval = 10 * time.Second
	}
	if cfg.AssignmentStrategy == "" {
		cfg.AssignmentStrategy = "least_loaded"
	}

	pool := &WorkerPoolService{
		workers: make(map[string]*models.WorkerNode),
		config:  cfg,
	}

	// 设置分配策略
	switch cfg.AssignmentStrategy {
	case "round_robin":
		pool.strategy = &RoundRobinStrategy{}
	case "least_loaded":
		pool.strategy = &LeastLoadedStrategy{}
	case "priority":
		pool.strategy = &PriorityStrategy{}
	case "random":
		pool.strategy = &RandomStrategy{}
	default:
		pool.strategy = &LeastLoadedStrategy{}
	}

	// 启动健康检查
	go pool.healthChecker()

	return pool
}

// RegisterWorker 注册 Worker
func (p *WorkerPoolService) RegisterWorker(worker *models.WorkerNode) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.workers) >= p.config.MaxWorkersPerPool {
		return fmt.Errorf("worker pool is full (max: %d)", p.config.MaxWorkersPerPool)
	}

	worker.Status = "idle"
	worker.RegisteredAt = time.Now()
	worker.LastHeartbeat = time.Now()
	if worker.MaxConcurrent == 0 {
		worker.MaxConcurrent = 1
	}

	p.workers[worker.ID] = worker
	log.Printf("[WorkerPool] Worker registered: %s (%s:%d, max_concurrent=%d)",
		worker.ID, worker.Address, worker.Port, worker.MaxConcurrent)
	return nil
}

// DeregisterWorker 注销 Worker
func (p *WorkerPoolService) DeregisterWorker(workerID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	worker, exists := p.workers[workerID]
	if !exists {
		return fmt.Errorf("worker not found: %s", workerID)
	}

	if worker.ActiveUnits > 0 {
		worker.Status = "draining"
		log.Printf("[WorkerPool] Worker %s set to draining (%d active units)", workerID, worker.ActiveUnits)
		return nil
	}

	delete(p.workers, workerID)
	log.Printf("[WorkerPool] Worker deregistered: %s", workerID)
	return nil
}

// UpdateHeartbeat 更新 Worker 心跳
func (p *WorkerPoolService) UpdateHeartbeat(workerID string, cpu, memory, disk float64) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	worker, exists := p.workers[workerID]
	if !exists {
		return fmt.Errorf("worker not found: %s", workerID)
	}

	worker.LastHeartbeat = time.Now()
	worker.CPUUsage = cpu
	worker.MemoryUsage = memory
	worker.DiskUsage = disk

	// 如果之前是 offline，恢复为 idle
	if worker.Status == "offline" {
		if worker.ActiveUnits > 0 {
			worker.Status = "busy"
		} else {
			worker.Status = "idle"
		}
		log.Printf("[WorkerPool] Worker %s recovered from offline", workerID)
	}

	return nil
}

// AssignUnit 分配单元任务到 Worker (ORCH-009)
func (p *WorkerPoolService) AssignUnit(unit *models.Unit) (*models.WorkerNode, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	available := p.getAvailableWorkers()
	if len(available) == 0 {
		return nil, fmt.Errorf("no available workers")
	}

	selected := p.strategy.Select(available)
	if selected == nil {
		return nil, fmt.Errorf("strategy returned no worker")
	}

	// 更新 Worker 状态
	selected.ActiveUnits++
	selected.CurrentUnitID = unit.ID
	if selected.ActiveUnits >= selected.MaxConcurrent {
		selected.Status = "busy"
	}

	// 更新 Unit 状态
	now := time.Now()
	unit.WorkerID = selected.ID
	unit.Status = models.UnitStatusAssigned
	unit.AssignedAt = &now

	log.Printf("[WorkerPool] Unit %s assigned to worker %s (strategy=%s, load=%d/%d)",
		unit.ID, selected.ID, p.strategy.Name(), selected.ActiveUnits, selected.MaxConcurrent)

	return selected, nil
}

// ReleaseUnit 释放单元任务
func (p *WorkerPoolService) ReleaseUnit(workerID, unitID string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	worker, exists := p.workers[workerID]
	if !exists {
		return
	}

	if worker.ActiveUnits > 0 {
		worker.ActiveUnits--
	}
	if worker.CurrentUnitID == unitID {
		worker.CurrentUnitID = ""
	}
	if worker.ActiveUnits == 0 {
		if worker.Status == "draining" {
			delete(p.workers, workerID)
			log.Printf("[WorkerPool] Drained worker removed: %s", workerID)
			return
		}
		worker.Status = "idle"
	}
}

// GetWorkers 获取所有 Worker
func (p *WorkerPoolService) GetWorkers() []*models.WorkerNode {
	p.mu.RLock()
	defer p.mu.RUnlock()

	workers := make([]*models.WorkerNode, 0, len(p.workers))
	for _, w := range p.workers {
		copy := *w
		workers = append(workers, &copy)
	}
	return workers
}

// GetWorker 获取单个 Worker
func (p *WorkerPoolService) GetWorker(workerID string) (*models.WorkerNode, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	w, exists := p.workers[workerID]
	if !exists {
		return nil, fmt.Errorf("worker not found: %s", workerID)
	}
	copy := *w
	return &copy, nil
}

// GetPoolStats 获取池统计
func (p *WorkerPoolService) GetPoolStats() *PoolStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := &PoolStats{
		TotalWorkers: len(p.workers),
		Strategy:     p.strategy.Name(),
	}

	for _, w := range p.workers {
		switch w.Status {
		case "idle":
			stats.IdleWorkers++
		case "busy":
			stats.BusyWorkers++
		case "offline":
			stats.OfflineWorkers++
		case "draining":
			stats.DrainingWorkers++
		}
		stats.TotalActiveUnits += w.ActiveUnits
		stats.TotalCapacity += w.MaxConcurrent
	}

	if stats.TotalCapacity > 0 {
		stats.Utilization = float64(stats.TotalActiveUnits) / float64(stats.TotalCapacity) * 100
	}

	return stats
}

// PoolStats 池统计
type PoolStats struct {
	TotalWorkers     int     `json:"total_workers"`
	IdleWorkers      int     `json:"idle_workers"`
	BusyWorkers      int     `json:"busy_workers"`
	OfflineWorkers   int     `json:"offline_workers"`
	DrainingWorkers  int     `json:"draining_workers"`
	TotalActiveUnits int     `json:"total_active_units"`
	TotalCapacity    int     `json:"total_capacity"`
	Utilization      float64 `json:"utilization_percent"`
	Strategy         string  `json:"strategy"`
}

// getAvailableWorkers 获取可用 Worker
func (p *WorkerPoolService) getAvailableWorkers() []*models.WorkerNode {
	cutoff := time.Now().Add(-p.config.HeartbeatTimeout)
	available := make([]*models.WorkerNode, 0)

	for _, w := range p.workers {
		if (w.Status == "idle" || w.Status == "busy") &&
			w.LastHeartbeat.After(cutoff) &&
			w.ActiveUnits < w.MaxConcurrent {
			available = append(available, w)
		}
	}
	return available
}

// healthChecker 健康检查
func (p *WorkerPoolService) healthChecker() {
	ticker := time.NewTicker(p.config.HealthCheckInterval)
	defer ticker.Stop()

	for range ticker.C {
		p.mu.Lock()
		cutoff := time.Now().Add(-p.config.HeartbeatTimeout)
		for id, w := range p.workers {
			if w.LastHeartbeat.Before(cutoff) && w.Status != "offline" {
				w.Status = "offline"
				log.Printf("[WorkerPool] Worker %s marked offline (last heartbeat: %s)", id, w.LastHeartbeat.Format(time.RFC3339))
			}
		}
		p.mu.Unlock()
	}
}

// ========== 分配策略实现 ==========

// RoundRobinStrategy 轮询策略
type RoundRobinStrategy struct {
	mu    sync.Mutex
	index int
}

func (s *RoundRobinStrategy) Name() string { return "round_robin" }

func (s *RoundRobinStrategy) Select(workers []*models.WorkerNode) *models.WorkerNode {
	if len(workers) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	selected := workers[s.index%len(workers)]
	s.index++
	return selected
}

// LeastLoadedStrategy 最小负载策略
type LeastLoadedStrategy struct{}

func (s *LeastLoadedStrategy) Name() string { return "least_loaded" }

func (s *LeastLoadedStrategy) Select(workers []*models.WorkerNode) *models.WorkerNode {
	if len(workers) == 0 {
		return nil
	}
	sort.Slice(workers, func(i, j int) bool {
		loadI := float64(workers[i].ActiveUnits) / float64(workers[i].MaxConcurrent)
		loadJ := float64(workers[j].ActiveUnits) / float64(workers[j].MaxConcurrent)
		return loadI < loadJ
	})
	return workers[0]
}

// PriorityStrategy 优先级策略（资源使用率低优先）
type PriorityStrategy struct{}

func (s *PriorityStrategy) Name() string { return "priority" }

func (s *PriorityStrategy) Select(workers []*models.WorkerNode) *models.WorkerNode {
	if len(workers) == 0 {
		return nil
	}
	sort.Slice(workers, func(i, j int) bool {
		scoreI := workers[i].CPUUsage*0.5 + workers[i].MemoryUsage*0.3 + float64(workers[i].ActiveUnits)/float64(workers[i].MaxConcurrent)*0.2*100
		scoreJ := workers[j].CPUUsage*0.5 + workers[j].MemoryUsage*0.3 + float64(workers[j].ActiveUnits)/float64(workers[j].MaxConcurrent)*0.2*100
		return scoreI < scoreJ
	})
	return workers[0]
}

// RandomStrategy 随机策略
type RandomStrategy struct{}

func (s *RandomStrategy) Name() string { return "random" }

func (s *RandomStrategy) Select(workers []*models.WorkerNode) *models.WorkerNode {
	if len(workers) == 0 {
		return nil
	}
	return workers[rand.Intn(len(workers))]
}
