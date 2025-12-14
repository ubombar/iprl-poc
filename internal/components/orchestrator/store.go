package orchestrator

import (
	"sync"

	pb "iprl-demo/internal/gen/proto"
)

type componentStore struct {
	mu              sync.RWMutex
	agentSpecs      map[string]*pb.ProbingAgentSpec
	agentStatus     map[string]*pb.ProbingAgentStatus
	generatorSpecs  map[string]*pb.ProbingDirectiveGeneratorSpec
	generatorStatus map[string]*pb.ProbingDirectiveGeneratorStatus

	changedMu         sync.Mutex
	changedAgents     map[string]struct{}
	changedGenerators map[string]struct{}
}

func newComponentStore() *componentStore {
	return &componentStore{
		agentSpecs:        make(map[string]*pb.ProbingAgentSpec),
		agentStatus:       make(map[string]*pb.ProbingAgentStatus),
		generatorSpecs:    make(map[string]*pb.ProbingDirectiveGeneratorSpec),
		generatorStatus:   make(map[string]*pb.ProbingDirectiveGeneratorStatus),
		changedAgents:     make(map[string]struct{}),
		changedGenerators: make(map[string]struct{}),
	}
}

func (c *componentStore) RegisterAgent(spec *pb.ProbingAgentSpec, status *pb.ProbingAgentStatus) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.agentSpecs[status.Uuid]; ok {
		return ErrComponentExists
	}

	c.agentSpecs[status.Uuid] = spec
	c.agentStatus[status.Uuid] = status
	c.markAgentChanged(status.Uuid)

	return nil
}

func (c *componentStore) RegisterGenerator(spec *pb.ProbingDirectiveGeneratorSpec, status *pb.ProbingDirectiveGeneratorStatus) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.generatorSpecs[status.Uuid]; ok {
		return ErrComponentExists
	}

	c.generatorSpecs[status.Uuid] = spec
	c.generatorStatus[status.Uuid] = status
	c.markGeneratorChanged(status.Uuid)

	return nil
}

func (c *componentStore) DeleteAgent(uuid string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.agentSpecs[uuid]; !ok {
		return ErrComponentNotFound
	}

	delete(c.agentSpecs, uuid)
	delete(c.agentStatus, uuid)

	c.changedMu.Lock()
	delete(c.changedAgents, uuid)
	c.changedMu.Unlock()

	return nil
}

func (c *componentStore) DeleteGenerator(uuid string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.generatorSpecs[uuid]; !ok {
		return ErrComponentNotFound
	}

	delete(c.generatorSpecs, uuid)
	delete(c.generatorStatus, uuid)

	c.changedMu.Lock()
	delete(c.changedGenerators, uuid)
	c.changedMu.Unlock()

	return nil
}

func (c *componentStore) GetAgent(uuid string) (*pb.ProbingAgentSpec, *pb.ProbingAgentStatus, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	spec, ok := c.agentSpecs[uuid]
	if !ok {
		return nil, nil, ErrComponentNotFound
	}

	return spec, c.agentStatus[uuid], nil
}

func (c *componentStore) GetGenerator(uuid string) (*pb.ProbingDirectiveGeneratorSpec, *pb.ProbingDirectiveGeneratorStatus, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	spec, ok := c.generatorSpecs[uuid]
	if !ok {
		return nil, nil, ErrComponentNotFound
	}

	return spec, c.generatorStatus[uuid], nil
}

func (c *componentStore) ListAgents() ([]*pb.ProbingAgentSpec, []*pb.ProbingAgentStatus) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	specs := make([]*pb.ProbingAgentSpec, 0, len(c.agentSpecs))
	statuses := make([]*pb.ProbingAgentStatus, 0, len(c.agentStatus))

	for uuid, spec := range c.agentSpecs {
		specs = append(specs, spec)
		if status, ok := c.agentStatus[uuid]; ok {
			statuses = append(statuses, status)
		}
	}

	return specs, statuses
}

func (c *componentStore) ListGenerators() ([]*pb.ProbingDirectiveGeneratorSpec, []*pb.ProbingDirectiveGeneratorStatus) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	specs := make([]*pb.ProbingDirectiveGeneratorSpec, 0, len(c.generatorSpecs))
	statuses := make([]*pb.ProbingDirectiveGeneratorStatus, 0, len(c.generatorStatus))

	for uuid, spec := range c.generatorSpecs {
		specs = append(specs, spec)
		if status, ok := c.generatorStatus[uuid]; ok {
			statuses = append(statuses, status)
		}
	}

	return specs, statuses
}

func (c *componentStore) GetChangedAgents() []*pb.ProbingAgentStatus {
	c.mu.RLock()
	c.changedMu.Lock()
	defer c.mu.RUnlock()
	defer c.changedMu.Unlock()

	var statuses []*pb.ProbingAgentStatus

	for uuid := range c.changedAgents {
		if status, ok := c.agentStatus[uuid]; ok {
			statuses = append(statuses, status)
		}
	}

	c.changedAgents = make(map[string]struct{})

	return statuses
}

func (c *componentStore) GetChangedGenerators() []*pb.ProbingDirectiveGeneratorStatus {
	c.mu.RLock()
	c.changedMu.Lock()
	defer c.mu.RUnlock()
	defer c.changedMu.Unlock()

	var statuses []*pb.ProbingDirectiveGeneratorStatus

	for uuid := range c.changedGenerators {
		if status, ok := c.generatorStatus[uuid]; ok {
			statuses = append(statuses, status)
		}
	}

	c.changedGenerators = make(map[string]struct{})

	return statuses
}

func (c *componentStore) markAgentChanged(uuid string) {
	c.changedMu.Lock()
	defer c.changedMu.Unlock()
	c.changedAgents[uuid] = struct{}{}
}

func (c *componentStore) markGeneratorChanged(uuid string) {
	c.changedMu.Lock()
	defer c.changedMu.Unlock()
	c.changedGenerators[uuid] = struct{}{}
}
