package hare

import (
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/require"
	"testing"
)

type mockOutput struct {
	id uint32
	vals map[uint32]Value
}

func (m mockOutput) Id() uint32 {
	return m.id
}
func (m mockOutput) Values() map[uint32]Value {
	return m.vals
}

func TestNew(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	layerTicker := make(chan mesh.LayerID)

	oracle := NewMockOracle()
	signing := NewMockSigning()

	om := new(orphanMock)

	h := New(n1, n1.PublicKey(), signing, om, oracle, layerTicker)

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

	h := New(n1, n1.PublicKey(), signing, om, oracle, layerTicker)

	h.b.Start() // todo: fix that hack. this will cause h.Start to return err

	err := h.Start()
	require.Error(t, err)

	h2 := New(n1, n1.PublicKey(), signing, om, oracle, layerTicker)
	require.NoError(t, h2.Start())
}

func TestHare_GetResult(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	layerTicker := make(chan mesh.LayerID)

	oracle := NewMockOracle()
	signing := NewMockSigning()

	om := new(orphanMock)

	h := New(n1, n1.PublicKey(), signing, om, oracle, layerTicker)

	res, err := h.GetResult(mesh.LayerID(0))

	require.Error(t, err)
	require.Nil(t, res)

	mockid := uint32(0)
	mockvals := make(map[uint32]Value)
	mockvals[0] = Value{NewBytes32([]byte{0,0,0})}

	tm := make(chan TerminationOutput)

	go func() {
		tm <- mockOutput{mockid, mockvals}
	}()

	h.collectOutput(tm)

	res, err = h.GetResult(mesh.LayerID(0))

	require.NoError(t, err)

	require.True(t, uint32(res[0]) == mockvals[0].Id())
}

func TestHare_collectOutput(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	layerTicker := make(chan mesh.LayerID)

	oracle := NewMockOracle()
	signing := NewMockSigning()

	om := new(orphanMock)

	h := New(n1, n1.PublicKey(), signing, om, oracle, layerTicker)

	mockid := uint32(0)
	mockvals := make(map[uint32]Value)
	mockvals[0] = Value{NewBytes32([]byte{0, 0, 0})}

	tm := make(chan TerminationOutput)

	go func() {
		tm <- mockOutput{mockid, mockvals}
	}()

	h.collectOutput(tm)
	output, ok := h.outputs[mesh.LayerID(mockid)]
	require.True(t, ok)
	require.True(t, output[0] == mesh.BlockID(mockvals[0].Id()))

	mockid = uint32(2)

	go func() {
		h.Close()
		tm <- mockOutput{mockid, mockvals} // should block forever
	}()

	output, ok = h.outputs[mesh.LayerID(mockid)] // todo : replace with getresult if this is yields a race
	require.False(t, ok)
	require.Nil(t, output)

}

func TestHare_onTick(t *testing.T) {
	sim := service.NewSimulator()
	n1 := sim.NewNode()

	layerTicker := make(chan mesh.LayerID)

	oracle := NewMockOracle()
	signing := NewMockSigning()

	called := false

	om := new(orphanMock)
	om.f = func() []mesh.BlockID {
		called = true
		return []mesh.BlockID{mesh.BlockID(0), mesh.BlockID(1), mesh.BlockID(2)}
	}

	h := New(n1, n1.PublicKey(), signing, om, oracle, layerTicker)

	h.Start()

	go func() {
		layerTicker <- mesh.LayerID(0)
	}()


	mockid := uint32(0)
	mockvals := make(map[uint32]Value)
	mockvals[0] = Value{NewBytes32([]byte{0, 0, 0})}

	tm := make(chan TerminationOutput)

	go func() {
		tm <- mockOutput{mockid, mockvals}
	}()

	h.collectOutput(tm)
}