package hare

import (
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/require"
	"sort"
	"sync"
	"testing"
	"time"
)

type mockOutput struct {
	id   []byte
	vals map[uint32]Value
}

func (m mockOutput) Id() []byte {
	return m.id
}
func (m mockOutput) Values() map[uint32]Value {
	return m.vals
}

type mockConsensusProcess struct {
	Closer
	t   chan TerminationOutput
	id  uint32
	set *Set
}

func (mcp *mockConsensusProcess) Start() error {
	mcp.t <- mockOutput{common.Uint32ToBytes(mcp.id), mcp.set.values}
	return nil
}

func (mcp *mockConsensusProcess) Id() uint32 {
	return mcp.id
}

func (mcp *mockConsensusProcess) TerminationOutput() chan TerminationOutput {
	return mcp.t
}

func (mcp *mockConsensusProcess) createInbox(size uint32) chan *pb.HareMessage {
	c := make(chan *pb.HareMessage)
	go func() {
		for {
			<-c
			// don't really need to use messages just don't block
		}
	}()
	return c
}

func NewMockConsensusProcess(cfg config.Config, key crypto.PublicKey, instanceId InstanceId, s *Set, oracle Rolacle, signing Signing, p2p NetworkService) *mockConsensusProcess {
	mcp := new(mockConsensusProcess)
	mcp.Closer = NewCloser()
	mcp.id = common.BytesToUint32(instanceId.Bytes())
	mcp.t = make(chan TerminationOutput)
	mcp.set = s
	return mcp
}

var _ Consensus = (*mockConsensusProcess)(nil)

func TestNew(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	layerTicker := make(chan mesh.LayerID)

	oracle := NewMockOracle()
	signing := NewMockSigning()

	om := new(orphanMock)

	h := New(cfg, n1, n1.PublicKey(), signing, om, oracle, layerTicker)

	if h == nil {
		t.Fatal()
	}
}

func TestHare_Start(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	layerTicker := make(chan mesh.LayerID)

	oracle := NewMockOracle()
	signing := NewMockSigning()

	om := new(orphanMock)

	h := New(cfg, n1, n1.PublicKey(), signing, om, oracle, layerTicker)

	h.b.Start() // todo: fix that hack. this will cause h.Start to return err

	err := h.Start()
	require.Error(t, err)

	h2 := New(cfg, n1, n1.PublicKey(), signing, om, oracle, layerTicker)
	require.NoError(t, h2.Start())
}

func TestHare_GetResult(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	layerTicker := make(chan mesh.LayerID)

	oracle := NewMockOracle()
	signing := NewMockSigning()

	om := new(orphanMock)

	h := New(cfg, n1, n1.PublicKey(), signing, om, oracle, layerTicker)

	res, err := h.GetResult(mesh.LayerID(0))

	require.Error(t, err)
	require.Nil(t, res)

	mockid := common.Uint32ToBytes(uint32(0))
	mockvals := make(map[uint32]Value)
	mockvals[0] = Value{NewBytes32([]byte{0, 0, 0})}

	tm := make(chan TerminationOutput)

	go func() {
		tm <- mockOutput{mockid, mockvals}
	}()

	h.collectOutput(tm)

	res, err = h.GetResult(mesh.LayerID(0))

	require.NoError(t, err)

	require.True(t, uint32(res[0]) == common.BytesToUint32(mockvals[0].Bytes()))
}

func TestHare_collectOutput(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	layerTicker := make(chan mesh.LayerID)

	oracle := NewMockOracle()
	signing := NewMockSigning()

	om := new(orphanMock)

	h := New(cfg, n1, n1.PublicKey(), signing, om, oracle, layerTicker)

	mockid := uint32(0)
	mockvals := make(map[uint32]Value)
	mockvals[0] = Value{NewBytes32([]byte{0, 0, 0})}

	var wg sync.WaitGroup

	tm := make(chan TerminationOutput)

	wg.Add(1)

	go func() {
		wg.Done()
		tm <- mockOutput{common.Uint32ToBytes(mockid), mockvals}
	}()

	wg.Wait()
	h.collectOutput(tm)
	output, ok := h.outputs[mesh.LayerID(mockid)]
	require.True(t, ok)
	require.Equal(t, output[0], mesh.BlockID(common.BytesToUint32(mockvals[0].Bytes())))

	mockid = uint32(2)

	wg.Add(1)
	go func() {
		h.Close()
		wg.Done()
	}()

	h.collectOutput(tm)
	wg.Wait()

	go func() {
		tm <- mockOutput{common.Uint32ToBytes(mockid), mockvals} // should block forever
	}()
	output, ok = h.outputs[mesh.LayerID(mockid)] // todo : replace with getresult if this is yields a race
	require.False(t, ok)
	require.Nil(t, output)

}

func TestHare_onTick(t *testing.T) {
	cfg := config.DefaultConfig()

	cfg.N = 2
	cfg.F = 1
	cfg.RoundDuration = time.Millisecond
	cfg.SetSize = 3

	sim := service.NewSimulator()
	n1 := sim.NewNode()

	layerTicker := make(chan mesh.LayerID)

	oracle := NewMockOracle()
	signing := NewMockSigning()

	blockset := []mesh.BlockID{mesh.BlockID(0), mesh.BlockID(1), mesh.BlockID(2)}
	om := new(orphanMock)
	om.f = func() []mesh.BlockID {
		return blockset
	}

	h := New(cfg, n1, n1.PublicKey(), signing, om, oracle, layerTicker)
	h.networkDelta = 0
	var nmcp *mockConsensusProcess
	h.factory = func(cfg config.Config, key crypto.PublicKey, instanceId InstanceId, s *Set, oracle Rolacle, signing Signing, p2p NetworkService) Consensus {
		nmcp = NewMockConsensusProcess(cfg, key, instanceId, s, oracle, signing, p2p)
		return nmcp
	}
	h.Start()

	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		wg.Done()
		layerTicker <- mesh.LayerID(0)
		wg.Done()
	}()

	//collect output one more time
	wg.Wait()

	time.Sleep(1 * time.Second)
	res2, err := h.GetResult(mesh.LayerID(0))
	require.NoError(t, err)

	SortBlockIDs(res2)
	SortBlockIDs(blockset)

	require.Equal(t, blockset, res2)

	wg.Add(2)
	go func() {
		wg.Done()
		layerTicker <- mesh.LayerID(1)
		h.Close()
		wg.Done()
	}()

	//collect output one more time
	wg.Wait()
	_, err = h.GetResult(mesh.LayerID(1))
	require.Error(t, err)

}

type BlockIDSlice []mesh.BlockID

func (p BlockIDSlice) Len() int           { return len(p) }
func (p BlockIDSlice) Less(i, j int) bool { return p[i] < p[j] }
func (p BlockIDSlice) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

// Sort is a convenience method.
func (p BlockIDSlice) Sort() { sort.Sort(p) }

func SortBlockIDs(slice []mesh.BlockID) {
	sort.Sort(BlockIDSlice(slice))
}
