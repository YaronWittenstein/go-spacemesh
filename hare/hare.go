package hare

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"sync"
	"time"
)

const Delta = time.Second // todo: add to config

type consensusFactory func(cfg config.Config, key crypto.PublicKey, instanceId InstanceId, s *Set, oracle Rolacle, signing Signing, p2p NetworkService) Consensus

type Consensus interface {
	IdentifiableInboxer

	Close()
	CloseChannel() chan struct{}
	Start() error

	Terminator
}

// Terminator is an entity that terminates with an output
type Terminator interface {
	TerminationOutput() chan TerminationOutput
}

// TerminationOutput is a result of a process terminated with output.
type TerminationOutput interface {
	Id() []byte
	Values() map[uint32]Value
}

type orphanBlockProvider interface {
	GetOrphanBlocks() []mesh.BlockID
}

// Hare is a wrapper that  orchestrator that shoots consensus processes and collects their termination output
type Hare struct {
	Closer

	config config.Config

	network    NetworkService
	beginLayer chan mesh.LayerID

	b *Broker

	me   crypto.PublicKey
	sign Signing

	obp     orphanBlockProvider
	rolacle Rolacle

	networkDelta time.Duration

	mu      sync.RWMutex
	outputs map[mesh.LayerID][]mesh.BlockID

	factory consensusFactory
}

// New returns a new Hare struct.
func New(conf config.Config, p2p NetworkService, me crypto.PublicKey, sign Signing, obp orphanBlockProvider, rolacle Rolacle, beginLayer chan mesh.LayerID) *Hare {
	h := new(Hare)
	h.Closer = NewCloser()

	h.config = conf

	h.network = p2p
	h.beginLayer = beginLayer

	h.b = NewBroker(p2p)

	h.me = me
	h.sign = sign

	h.obp = obp
	h.rolacle = rolacle

	h.networkDelta = Delta

	h.outputs = make(map[mesh.LayerID][]mesh.BlockID)

	h.factory = func(conf config.Config, key crypto.PublicKey, instanceId InstanceId, s *Set, oracle Rolacle, signing Signing, p2p NetworkService) Consensus {
		return NewConsensusProcess(conf, key, instanceId, s, oracle, signing, p2p)
	}

	return h
}

func (h *Hare) collectOutput(box chan TerminationOutput) {
	var out TerminationOutput
	// todo: do we want to ever give up on waiting for hare ?
	select {
	case out = <-box:
		break // keep going
	case <-h.CloseChannel():
		// closed while waiting the delta
		return
	}
	id := out.Id()
	v := out.Values()
	blocks := make([]mesh.BlockID, len(v))
	i := 0
	for _, vv := range v {
		blocks[i] = mesh.BlockID(common.BytesToUint32(vv.Bytes()))
		i++
	}
	h.mu.Lock()
	h.outputs[mesh.LayerID(common.BytesToUint32(id))] = blocks
	h.mu.Unlock()
}

func (h *Hare) onTick(id mesh.LayerID) {
	ti := time.NewTimer(h.networkDelta)
	select {
	case <-ti.C:
		break // keep going
	case <-h.CloseChannel():
		// closed while waiting the delta
		return
	}

	// retrieve set form orphan blocks
	blocks := h.obp.GetOrphanBlocks()

	set := NewEmptySet(len(blocks))

	for _, b := range blocks {
		// todo: figure out real type of blockid
		set.Add(Value{NewBytes32(b.ToBytes())})
	}

	instid := InstanceId{NewBytes32(id.ToBytes())}

	cp := h.factory(h.config, h.me, instid, set, h.rolacle, h.sign, h.network)
	go h.collectOutput(cp.TerminationOutput())
	h.b.Register(cp)
	cp.Start()
}

// GetResults returns the hare output for a given LayerID. returns error if we don't have results yet.
func (h *Hare) GetResult(id mesh.LayerID) ([]mesh.BlockID, error) {
	h.mu.RLock()
	blks, ok := h.outputs[id]
	h.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("didn't get results for layer %v yet", id)
	}
	return blks, nil
}

func (h *Hare) tickLoop() {
	for {
		select {
		case layer := <-h.beginLayer:
			h.onTick(layer)
		case <-h.CloseChannel():
			return
		}
	}
}

// Start starts listening on layers to participate in.
func (h *Hare) Start() error {
	err := h.b.Start()
	if err != nil {
		return err
	}
	go h.tickLoop()
	return nil
}
