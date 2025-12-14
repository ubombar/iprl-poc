package orchestrator

import (
	"sync"

	pb "iprl-demo/internal/gen/proto"
)

type componentStore struct {
	mu sync.RWMutex

	agentSpecs      map[string]*pb.ProbingAgentSpec
	agentStatus     map[string]*pb.ProbingAgentStatus
	generatorSpecs  map[string]*pb.ProbingDirectiveGeneratorSpec
	generatorStatus map[string]*pb.ProbingDirectiveGeneratorStatus

	dirtyAgentUuids     map[string]struct{}
	dirtyGeneratorUuids map[string]struct{}
}

func newComponentStore() *componentStore {
	return &componentStore{
		agentSpecs:      make(map[string]*pb.ProbingAgentSpec),
		agentStatus:     make(map[string]*pb.ProbingAgentStatus),
		generatorSpecs:  make(map[string]*pb.ProbingDirectiveGeneratorSpec),
		generatorStatus: make(map[string]*pb.ProbingDirectiveGeneratorStatus),

		dirtyAgentUuids:     make(map[string]struct{}),
		dirtyGeneratorUuids: make(map[string]struct{}),
	}
}

// AddOrUpdateAgent adds or updates an agent in the store.
// If agent exists: updates spec and status, marks agent as dirty.
// If agent is new: inserts spec and status, does NOT mark agent as dirty,
// but adds agent spec to ALL generators and marks them as dirty.
func (c *componentStore) AddOrUpdateAgent(spec *pb.ProbingAgentSpec, status *pb.ProbingAgentStatus) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	_, exists := c.agentSpecs[status.Uuid]

	c.agentSpecs[status.Uuid] = spec
	c.agentStatus[status.Uuid] = status

	if exists {
		// Existing agent updated - mark agent as dirty
		c.dirtyAgentUuids[status.Uuid] = struct{}{}

		// Also update spec in all generators' AgentSpecs list
		for _, genStatus := range c.generatorStatus {
			for i, agentSpec := range genStatus.AgentSpecs {
				if agentSpec.GrpcAddress == spec.GrpcAddress {
					genStatus.AgentSpecs[i] = spec
					break
				}
			}
		}
	} else {
		// New agent added - add to all generators and mark them as dirty
		for uuid, genStatus := range c.generatorStatus {
			// Check if agent already exists in generator's list
			alreadyExists := false
			for _, agentSpec := range genStatus.AgentSpecs {
				if agentSpec.GrpcAddress == spec.GrpcAddress {
					alreadyExists = true
					break
				}
			}

			if !alreadyExists {
				genStatus.AgentSpecs = append(genStatus.AgentSpecs, spec)
			}

			c.dirtyGeneratorUuids[uuid] = struct{}{}
		}
	}

	return nil
}

// RemoveAgent removes an agent from the store.
// Removes agent spec from ALL generators and marks them as dirty.
func (c *componentStore) RemoveAgent(uuid string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	spec, ok := c.agentSpecs[uuid]
	if !ok {
		return ErrComponentNotFound
	}

	delete(c.agentSpecs, uuid)
	delete(c.agentStatus, uuid)
	delete(c.dirtyAgentUuids, uuid)

	// Remove agent spec from all generators and mark them as dirty
	for genUuid, genStatus := range c.generatorStatus {
		for i, agentSpec := range genStatus.AgentSpecs {
			if agentSpec.GrpcAddress == spec.GrpcAddress {
				// Remove from slice
				genStatus.AgentSpecs = append(genStatus.AgentSpecs[:i], genStatus.AgentSpecs[i+1:]...)
				break
			}
		}
		c.dirtyGeneratorUuids[genUuid] = struct{}{}
	}

	return nil
}

// AddOrUpdateGenerator adds or updates a generator in the store.
// If generator is new: populates AgentSpecs with all current agents.
// If generator exists: updates spec and status, marks generator as dirty.
func (c *componentStore) AddOrUpdateGenerator(spec *pb.ProbingDirectiveGeneratorSpec, status *pb.ProbingDirectiveGeneratorStatus) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	_, exists := c.generatorSpecs[status.Uuid]

	c.generatorSpecs[status.Uuid] = spec

	if exists {
		// Existing generator updated - mark as dirty
		c.generatorStatus[status.Uuid] = status
		c.dirtyGeneratorUuids[status.Uuid] = struct{}{}
	} else {
		// New generator - populate AgentSpecs with all current agents
		agentSpecs := make([]*pb.ProbingAgentSpec, 0, len(c.agentSpecs))
		for _, agentSpec := range c.agentSpecs {
			agentSpecs = append(agentSpecs, agentSpec)
		}
		status.AgentSpecs = agentSpecs
		c.generatorStatus[status.Uuid] = status
		// Do not mark as dirty - it was just created with current state
	}

	return nil
}

// RemoveGenerator removes a generator from the store.
func (c *componentStore) RemoveGenerator(uuid string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.generatorSpecs[uuid]; !ok {
		return ErrComponentNotFound
	}

	delete(c.generatorSpecs, uuid)
	delete(c.generatorStatus, uuid)
	delete(c.dirtyGeneratorUuids, uuid)

	return nil
}

// GetAgent returns the spec and status for an agent.
func (c *componentStore) GetAgent(uuid string) (*pb.ProbingAgentSpec, *pb.ProbingAgentStatus, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	spec, ok := c.agentSpecs[uuid]
	if !ok {
		return nil, nil, ErrComponentNotFound
	}

	return spec, c.agentStatus[uuid], nil
}

// GetGenerator returns the spec and status for a generator.
func (c *componentStore) GetGenerator(uuid string) (*pb.ProbingDirectiveGeneratorSpec, *pb.ProbingDirectiveGeneratorStatus, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	spec, ok := c.generatorSpecs[uuid]
	if !ok {
		return nil, nil, ErrComponentNotFound
	}

	return spec, c.generatorStatus[uuid], nil
}

// UpdateAgentStatus updates only the status of an agent and marks it as dirty.
func (c *componentStore) UpdateAgentStatus(uuid string, status *pb.ProbingAgentStatus) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.agentSpecs[uuid]; !ok {
		return ErrComponentNotFound
	}

	status.Uuid = uuid
	c.agentStatus[uuid] = status
	c.dirtyAgentUuids[uuid] = struct{}{}

	return nil
}

// UpdateGeneratorStatus updates only the status of a generator and marks it as dirty.
func (c *componentStore) UpdateGeneratorStatus(uuid string, status *pb.ProbingDirectiveGeneratorStatus) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.generatorSpecs[uuid]; !ok {
		return ErrComponentNotFound
	}

	status.Uuid = uuid
	c.generatorStatus[uuid] = status
	c.dirtyGeneratorUuids[uuid] = struct{}{}

	return nil
}

// ListAgents returns all agent specs.
func (c *componentStore) ListAgents() []*pb.ProbingAgentSpec {
	c.mu.RLock()
	defer c.mu.RUnlock()

	specs := make([]*pb.ProbingAgentSpec, 0, len(c.agentSpecs))
	for _, spec := range c.agentSpecs {
		specs = append(specs, spec)
	}

	return specs
}

// ListGenerators returns all generator specs.
func (c *componentStore) ListGenerators() []*pb.ProbingDirectiveGeneratorSpec {
	c.mu.RLock()
	defer c.mu.RUnlock()

	specs := make([]*pb.ProbingDirectiveGeneratorSpec, 0, len(c.generatorSpecs))
	for _, spec := range c.generatorSpecs {
		specs = append(specs, spec)
	}

	return specs
}

// GetDirtyAgentUuids returns UUIDs of dirty agents and clears their dirty flags.
func (c *componentStore) GetDirtyAgentUuids() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	uuids := make([]string, 0, len(c.dirtyAgentUuids))
	for uuid := range c.dirtyAgentUuids {
		if _, ok := c.agentStatus[uuid]; ok {
			uuids = append(uuids, uuid)
		}
	}

	c.dirtyAgentUuids = make(map[string]struct{})

	return uuids
}

// GetDirtyGeneratorUuids returns UUIDs of dirty generators and clears their dirty flags.
func (c *componentStore) GetDirtyGeneratorUuids() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	uuids := make([]string, 0, len(c.dirtyGeneratorUuids))
	for uuid := range c.dirtyGeneratorUuids {
		if _, ok := c.generatorStatus[uuid]; ok {
			uuids = append(uuids, uuid)
		}
	}

	c.dirtyGeneratorUuids = make(map[string]struct{})

	return uuids
}

// AgentCount returns the number of registered agents.
func (c *componentStore) AgentCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.agentSpecs)
}

// GeneratorCount returns the number of registered generators.
func (c *componentStore) GeneratorCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.generatorSpecs)
}

// HasDirtyComponents returns true if there are any dirty agents or generators.
func (c *componentStore) HasDirtyComponents() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.dirtyAgentUuids) > 0 || len(c.dirtyGeneratorUuids) > 0
}
