package message

import (
	"encoding/hex"
	"fmt"
	"github.com/gogo/protobuf/proto"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/cryptoSign"
	"github.com/spacemeshos/go-spacemesh/p2p/pb"
	"time"
)

// NewProtocolMessageMetadata creates meta-data for an outgoing protocol message authored by this node.
func NewProtocolMessageMetadata(author cryptoSign.PublicKey, protocol string) *pb.Metadata {
	return &pb.Metadata{
		NextProtocol:  protocol,
		ClientVersion: config.ClientVersion,
		Timestamp:     time.Now().Unix(),
		AuthPubKey:    author.Bytes(),
	}
}

// SignMessage signs a message with a privatekey.
func SignMessage(pv cryptoSign.PrivateKey, pm *pb.ProtocolMessage) error {
	data, err := proto.Marshal(pm)
	if err != nil {
		e := fmt.Errorf("invalid msg format %v", err)
		return e
	}

	sign := pv.Sign(data)

	pm.Metadata.MsgSign = sign // TODO: `sign` is currently the signed message, not the message signature

	return nil
}

// AuthAuthor authorizes that a message is signed by its claimed author
func AuthAuthor(pm *pb.ProtocolMessage) error {
	// TODO
	if pm == nil || pm.Metadata == nil {
		return fmt.Errorf("can't sign defected message, message or metadata was empty")
	}

	sign := pm.Metadata.MsgSign
	sPubkey := pm.Metadata.AuthPubKey

	pubkey, err := crypto.NewPublicKey(sPubkey)
	if err != nil {
		return fmt.Errorf("could'nt create public key from %v, err: %v", hex.EncodeToString(sPubkey), err)
	}

	pm.Metadata.MsgSign = nil // we have to verify the message without the sign

	bin, err := proto.Marshal(pm)

	if err != nil {
		return err
	}

	v, err := pubkey.Verify(bin, sign)

	if err != nil {
		return err
	}

	if !v {
		return fmt.Errorf("coudld'nt verify message")
	}

	pm.Metadata.MsgSign = sign // restore sign because maybe we'll send it again ( gossip )

	return nil
}