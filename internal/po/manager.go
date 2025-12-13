package po

import (
	"context"
	"errors"
	"sync"

	"iprl-demo/internal/api"
	pb "iprl-demo/internal/gen/proto"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	ErrAgentAlreadyExists     = errors.New("agent already exists")
	ErrAgentNotFound          = errors.New("agent not found")
	ErrGeneratorAlreadyExists = errors.New("generator already exists")
	ErrGeneratorNotFound      = errors.New("generator not found")
)

type POManager struct {
	cfg                        *api.POConfig
	rwMutex                    sync.RWMutex
	probingAgentPool           map[string]*pb.ProbingAgent
	probingDirectiveGenerators map[string]*pb.ProbingDirectiveGenerator
	version                    uint64

	directiveCh chan *pb.ProbingDirective
	elementCh   chan *pb.ForwardingInfoElement
}

func NewPOManager(cfg *api.POConfig) *POManager {
	return &POManager{
		cfg:                        cfg,
		probingAgentPool:           make(map[string]*pb.ProbingAgent),
		probingDirectiveGenerators: make(map[string]*pb.ProbingDirectiveGenerator),
		version:                    1,

		// channels
		directiveCh: make(chan *pb.ProbingDirective),
		elementCh:   make(chan *pb.ForwardingInfoElement),
	}
}

func (pom *POManager) GetDirectiveChannel() chan *pb.ProbingDirective {
	return pom.directiveCh
}

func (pom *POManager) GetElementChannel() chan *pb.ForwardingInfoElement {
	return pom.elementCh
}

func (pom *POManager) AddPA(pa *pb.ProbingAgent) (*pb.ProbingAgent, error) {
	pom.rwMutex.Lock()
	defer pom.rwMutex.Unlock()

	if _, ok := pom.probingAgentPool[pa.Id]; ok {
		return nil, ErrAgentAlreadyExists
	}

	pom.probingAgentPool[pa.Id] = pa

	// Load default values if not set
	pa.Id = uuid.New().String()

	return pa, nil
}

func (pom *POManager) RemovePA(paID string) error {
	pom.rwMutex.Lock()
	defer pom.rwMutex.Unlock()
	if _, ok := pom.probingAgentPool[paID]; !ok {
		return ErrAgentNotFound
	}
	delete(pom.probingAgentPool, paID)
	return nil
}

func (pom *POManager) AddPDG(pdg *pb.ProbingDirectiveGenerator) (*pb.ProbingDirectiveGenerator, error) {
	pom.rwMutex.Lock()
	defer pom.rwMutex.Unlock()
	if _, ok := pom.probingDirectiveGenerators[pdg.Id]; ok {
		return nil, ErrGeneratorAlreadyExists
	}
	pom.probingDirectiveGenerators[pdg.Id] = pdg

	// Load the default values if applies
	pdg.Id = uuid.New().String()

	return pdg, nil
}

func (pom *POManager) RemovePDG(pdgID string) error {
	pom.rwMutex.Lock()
	defer pom.rwMutex.Unlock()
	if _, ok := pom.probingDirectiveGenerators[pdgID]; !ok {
		return ErrGeneratorNotFound
	}
	delete(pom.probingDirectiveGenerators, pdgID)
	return nil
}

func (pom *POManager) GetPA(paID string) (*pb.ProbingAgent, error) {
	pom.rwMutex.RLock()
	defer pom.rwMutex.RUnlock()
	pa, ok := pom.probingAgentPool[paID]
	if !ok {
		return &pb.ProbingAgent{}, ErrAgentNotFound
	}
	return pa, nil
}

func (pom *POManager) ListPAs() []*pb.ProbingAgent {
	pom.rwMutex.RLock()
	defer pom.rwMutex.RUnlock()
	agents := make([]*pb.ProbingAgent, 0, len(pom.probingAgentPool))
	for _, pa := range pom.probingAgentPool {
		agents = append(agents, pa)
	}
	return agents
}

func (pom *POManager) GetClusterStatus() *pb.ClusterStatus {
	pom.rwMutex.RLock()
	defer func() {
		pom.rwMutex.RUnlock()
		pom.version += 1
	}()

	agents := make([]*pb.ProbingAgent, 0, len(pom.probingAgentPool))
	for _, pa := range pom.probingAgentPool {
		agents = append(agents, pa)
	}

	generators := make([]*pb.ProbingDirectiveGenerator, 0, len(pom.probingDirectiveGenerators))
	for _, pdg := range pom.probingDirectiveGenerators {
		generators = append(generators, pdg)
	}

	return &pb.ClusterStatus{
		Agents:     agents,
		Generators: generators,
		UpdatedAt:  timestamppb.Now(),
		Version:    pom.version,
	}
}

func (pom *POManager) Run(ctx context.Context) {
}
