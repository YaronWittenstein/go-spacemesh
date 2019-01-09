package gossip

import (
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/cryptoSign"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/assert"
	"sync"
	"testing"
	"time"
)

type mockBaseNetwork struct {
	msgSentByPeer        map[cryptoSign.PublicKey]uint32
	inbox                chan service.Message
	connSubs             []chan cryptoSign.PublicKey
	discSubs             []chan cryptoSign.PublicKey
	totalMsgCount        int
	processProtocolCount int
	msgMutex             sync.Mutex
	pcountwg             *sync.WaitGroup
	msgwg                *sync.WaitGroup
	lastMsg              []byte
}

func newMockBaseNetwork() *mockBaseNetwork {
	return &mockBaseNetwork{
		make(map[cryptoSign.PublicKey]uint32),
		make(chan service.Message, 30),
		make([]chan cryptoSign.PublicKey, 0, 5),
		make([]chan cryptoSign.PublicKey, 0, 5),
		0,
		0,
		sync.Mutex{},
		&sync.WaitGroup{},
		&sync.WaitGroup{},
		[]byte(nil),
	}
}

func (mbn *mockBaseNetwork) SendMessage(peerPubKey cryptoSign.PublicKey, protocol string, payload []byte) error {
	mbn.msgMutex.Lock()
	mbn.lastMsg = payload
	mbn.msgSentByPeer[peerPubKey]++
	mbn.totalMsgCount++
	mbn.msgMutex.Unlock()
	releaseWaiters(mbn.msgwg)
	return nil
}

func passOrDeadlock(t testing.TB, group *sync.WaitGroup) {
	ch := make(chan struct{})
	go func(ch chan struct{}, t testing.TB) {
		timer := time.NewTimer(time.Second * 3)
		for {
			select {
			case <-ch:
				return
			case <-timer.C:
				t.FailNow() // deadlocked
			}
		}
	}(ch, t)

	group.Wait()
	close(ch)
}

// we use releaseWaiters to release a waitgroup and not panic if we don't use it
func releaseWaiters(group *sync.WaitGroup) {
	group.Done()
}

func (mbn *mockBaseNetwork) RegisterProtocol(protocol string) chan service.Message {
	return mbn.inbox
}

func (mbn *mockBaseNetwork) SubscribePeerEvents() (conn chan cryptoSign.PublicKey, disc chan cryptoSign.PublicKey) {
	conn = make(chan cryptoSign.PublicKey, 20)
	disc = make(chan cryptoSign.PublicKey, 20)

	mbn.connSubs = append(mbn.connSubs, conn)
	mbn.discSubs = append(mbn.discSubs, disc)
	return
}

func (mbn *mockBaseNetwork) ProcessProtocolMessage(sender node.Node, protocol string, data service.Data) error {
	mbn.processProtocolCount++
	releaseWaiters(mbn.pcountwg)
	return nil
}

func (mbn *mockBaseNetwork) addRandomPeers(cnt int) {
	for i := 0; i < cnt; i++ {
		_, pub, _ := cryptoSign.GenerateKeyPair()
		mbn.addRandomPeer(pub)
	}
}

func (mbn *mockBaseNetwork) addRandomPeer(pub cryptoSign.PublicKey) {
	for _, p := range mbn.connSubs {
		p <- pub
	}
}

func (mbn *mockBaseNetwork) totalMessageSent() int {
	mbn.msgMutex.Lock()
	total := mbn.totalMsgCount
	mbn.msgMutex.Unlock()
	return total
}

type mockSampler struct {
	f func(count int) []node.Node
}

func (mcs *mockSampler) SelectPeers(count int) []node.Node {
	if mcs.f != nil {
		return mcs.f(count)
	}
	return node.GenerateRandomNodesData(count)
}

type TestMessage struct {
	data service.Data
}

func (tm TestMessage) Sender() node.Node {
	return node.Node{}
}

func (tm TestMessage) setData(msg service.Data) {
	tm.data = msg
}

func (tm TestMessage) Data() service.Data {
	return tm.data
}

func (tm TestMessage) Bytes() []byte {
	return tm.data.Bytes()
}

type testSigner struct {
	priv cryptoSign.PrivateKey
	pub cryptoSign.PublicKey
}

func (ms testSigner) PublicKey() cryptoSign.PublicKey {
	return ms.pub
}

func (ms testSigner) Sign(data []byte) []byte {
	return ms.priv.Sign(data)
}

func newTestSigner(t testing.TB) testSigner {
	priv, pub, err := cryptoSign.GenerateKeyPair()
	assert.NoError(t, err)
	return testSigner{priv, pub}
}

func newTestSignedMessageData(t testing.TB, signer signer) []byte {
	pm := &pb.ProtocolMessage{
		Metadata: &pb.Metadata{
			NextProtocol:  ProtocolName,
			AuthPubKey:    signer.PublicKey().Bytes(),
			Timestamp:     time.Now().Unix(),
			ClientVersion: protocolVer,
		},
		Data: &pb.ProtocolMessage_Payload{[]byte("LOL")},
	}

	return signedMessage(t, signer, pm).Bytes()
}

func addPeersAndTest(t testing.TB, num int, p *Protocol, net *mockBaseNetwork, work bool) {

	pc := p.peersCount()
	reg, _ := net.SubscribePeerEvents()
	net.addRandomPeers(num)

	i := 0
lop:
	for {
		select {
		case <-reg:
			i++
			time.Sleep(time.Millisecond) // we need to somehow let other goroutines work before us
		default:
			break lop
		}
	}

	if i != num {
		t.Fatal("Didn't get added peers on chan")
	}

	newpc := p.peersCount()
	worked := pc+num == newpc
	if worked != work {
		t.Fatalf("adding the peers didn't work as expected old peer count: %d, tried to add: %d, new peercount: %d", pc, num, newpc)
	}
}

//todo : more unit tests

func TestNeighborhood_AddIncomingPeer(t *testing.T) {
	n := NewProtocol(config.DefaultConfig().SwarmConfig, newMockBaseNetwork(), newTestSigner(t), log.New("tesT", "", ""))
	n.Start()
	_, pub, _ := cryptoSign.GenerateKeyPair()
	n.addPeer(pub)

	assert.True(t, n.hasPeer(pub))
	assert.Equal(t, 1, n.peersCount())
}

func signedMessage(t testing.TB, s signer, message *pb.ProtocolMessage) service.Data {
	pmbin, err := proto.Marshal(message)
	assert.NoError(t, err)
	sign := s.Sign(pmbin)
	assert.NoError(t, err)
	message.Metadata.MsgSign = sign
	finbin, err := proto.Marshal(message)
	assert.NoError(t, err)
	return service.DataBytes{finbin}
}

func TestNeighborhood_Relay(t *testing.T) {
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, newTestSigner(t), log.New("tesT", "", ""))
	n.Start()

	addPeersAndTest(t, 20, n, net, true)

	signer := newTestSigner(t)
	pm := &pb.ProtocolMessage{
		Metadata: &pb.Metadata{
			NextProtocol:  ProtocolName,
			AuthPubKey:    signer.PublicKey().Bytes(),
			Timestamp:     time.Now().Unix(),
			ClientVersion: protocolVer,
		},
		Data: &pb.ProtocolMessage_Payload{[]byte("LOL")},
	}

	signed := signedMessage(t, signer, pm)

	var msg service.Message = TestMessage{signed}
	net.pcountwg.Add(1)
	net.msgwg.Add(20)
	net.inbox <- msg
	passOrDeadlock(t, net.pcountwg)
	passOrDeadlock(t, net.msgwg)
	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t, 20, net.totalMsgCount)
}

func TestNeighborhood_Broadcast(t *testing.T) {
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, newTestSigner(t), log.New("tesT", "", ""))
	n.Start()
	addPeersAndTest(t, 20, n, net, true)
	net.msgwg.Add(20)

	n.Broadcast([]byte("LOL"), "")
	passOrDeadlock(t, net.msgwg)
	assert.Equal(t, 0, net.processProtocolCount)
	assert.Equal(t, 20, net.totalMessageSent())
}

func TestNeighborhood_Relay2(t *testing.T) {
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, newTestSigner(t), log.New("tesT", "", ""))
	n.Start()

	signer := newTestSigner(t)
	pm := &pb.ProtocolMessage{
		Metadata: &pb.Metadata{
			NextProtocol:  ProtocolName,
			AuthPubKey:    signer.PublicKey().Bytes(),
			Timestamp:     time.Now().Unix(),
			ClientVersion: protocolVer,
		},
		Data: &pb.ProtocolMessage_Payload{[]byte("LOL")},
	}

	signed := signedMessage(t, signer, pm)
	var msg service.Message = TestMessage{signed}
	net.pcountwg.Add(1)
	net.inbox <- msg
	passOrDeadlock(t, net.pcountwg)
	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t, 0, net.totalMessageSent())

	addPeersAndTest(t, 20, n, net, true)
	net.msgwg.Add(20)
	net.inbox <- msg
	passOrDeadlock(t, net.msgwg)
	assert.Equal(t, 20, net.totalMessageSent())
}

func TestNeighborhood_Broadcast2(t *testing.T) {
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, newTestSigner(t), log.New("tesT", "", ""))
	n.Start()

	msgB := newTestSignedMessageData(t, newTestSigner(t))
	addPeersAndTest(t, 1, n, net, true)
	net.msgwg.Add(1)
	n.Broadcast(msgB, "") // dosent matter
	passOrDeadlock(t, net.msgwg)
	assert.Equal(t, 0, net.processProtocolCount)
	assert.Equal(t, 1, net.totalMessageSent())

	addPeersAndTest(t, 20, n, net, true)
	net.msgwg.Add(20)
	var msg service.Message = TestMessage{service.DataBytes{net.lastMsg}}
	net.inbox <- msg
	passOrDeadlock(t, net.msgwg)
	assert.Equal(t, 0, net.processProtocolCount)
	assert.Equal(t, 21, net.totalMessageSent())
}

func TestNeighborhood_Broadcast3(t *testing.T) {
	// todo : Fix this test, because the first message is broadcasted `Broadcast` attaches metadata to it with the current authoring timestamp
	// to test that the the next message doesn't get processed by the protocol we must create an exact copy of the message produced at `Broadcast`
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, newTestSigner(t), log.New("tesT", "", ""))
	n.Start()

	addPeersAndTest(t, 20, n, net, true)

	msgB := []byte("LOL")
	net.msgwg.Add(20)
	n.Broadcast(msgB, "")
	passOrDeadlock(t, net.msgwg)
	assert.Equal(t, 0, net.processProtocolCount)
	assert.Equal(t, 20, net.totalMessageSent())

	var msg service.Message = TestMessage{service.DataBytes{net.lastMsg}}
	net.inbox <- msg
	assert.Equal(t, 0, net.processProtocolCount)
	assert.Equal(t, 20, net.totalMessageSent())
}

func TestNeighborhood_Relay3(t *testing.T) {
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, newTestSigner(t), log.New("tesT", "", ""))
	n.Start()

	var msg service.Message = TestMessage{service.DataBytes{newTestSignedMessageData(t, newTestSigner(t))}}
	net.pcountwg.Add(1)
	net.inbox <- msg
	passOrDeadlock(t, net.pcountwg)
	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t, 0, net.totalMessageSent())

	addPeersAndTest(t, 20, n, net, true)

	net.msgwg.Add(20)
	net.inbox <- msg
	passOrDeadlock(t, net.msgwg)
	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t, 20, net.totalMessageSent())

	addPeersAndTest(t, 1, n, net, true)

	net.msgwg.Add(1)
	net.inbox <- msg
	passOrDeadlock(t, net.msgwg)

	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t, 21, net.totalMessageSent())
}

func TestNeighborhood_Start(t *testing.T) {
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, newTestSigner(t), log.New("tesT", "", ""))

	// before Start
	addPeersAndTest(t, 20, n, net, false)

	n.Start()

	addPeersAndTest(t, 20, n, net, true)
}

func TestNeighborhood_Close(t *testing.T) {
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, newTestSigner(t), log.New("tesT", "", ""))

	n.Start()
	addPeersAndTest(t, 20, n, net, true)

	n.Close()
	addPeersAndTest(t, 20, n, net, false)
}

func TestNeighborhood_Disconnect(t *testing.T) {
	net := newMockBaseNetwork()
	n := NewProtocol(config.DefaultConfig().SwarmConfig, net, newTestSigner(t), log.New("tesT", "", ""))

	n.Start()
	_, pub1, _ := cryptoSign.GenerateKeyPair()
	n.addPeer(pub1)
	_, pub2, _ := cryptoSign.GenerateKeyPair()
	n.addPeer(pub2)
	assert.Equal(t, 2, n.peersCount())

	msg := newTestSignedMessageData(t, newTestSigner(t))

	net.pcountwg.Add(1)
	net.msgwg.Add(2)
	net.inbox <- TestMessage{service.DataBytes{msg}}
	passOrDeadlock(t, net.pcountwg)
	passOrDeadlock(t, net.msgwg)
	assert.Equal(t, 1, net.processProtocolCount)
	assert.Equal(t, 2, net.totalMessageSent())

	msg2 := newTestSignedMessageData(t, newTestSigner(t))

	n.removePeer(pub1)
	net.pcountwg.Add(1)
	net.msgwg.Add(1)
	net.inbox <- TestMessage{service.DataBytes{msg2}}
	passOrDeadlock(t, net.pcountwg)
	passOrDeadlock(t, net.msgwg)
	assert.Equal(t, 2, net.processProtocolCount)
	assert.Equal(t, 3, net.totalMessageSent())

	n.addPeer(pub1)
	net.msgwg.Add(1)
	net.inbox <- TestMessage{service.DataBytes{msg2}}
	passOrDeadlock(t, net.msgwg)
	assert.Equal(t, 2, net.processProtocolCount)
	assert.Equal(t, 4, net.totalMessageSent())
}
