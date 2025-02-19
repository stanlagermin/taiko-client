package evidence

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/taikoxyz/taiko-client/bindings"
	"github.com/taikoxyz/taiko-client/bindings/encoding"
	"github.com/taikoxyz/taiko-client/pkg/rpc"
	anchorTxValidator "github.com/taikoxyz/taiko-client/prover/anchor_tx_validator"
	proofProducer "github.com/taikoxyz/taiko-client/prover/proof_producer"
)

// Builder is responsible for building evidence for the given L2 block proof.
type Builder struct {
	rpc               *rpc.Client
	anchorTxValidator *anchorTxValidator.AnchorTxValidator
	graffiti          [32]byte
}

// NewBuilder creates a new Builder instance.
func NewBuilder(cli *rpc.Client, anchorTxValidator *anchorTxValidator.AnchorTxValidator, graffiti string) *Builder {
	return &Builder{
		rpc:               cli,
		anchorTxValidator: anchorTxValidator,
		graffiti:          rpc.StringToBytes32(graffiti),
	}
}

// ForSubmission creates the evidence for the given L2 block proof.
func (b *Builder) ForSubmission(
	ctx context.Context,
	proofWithHeader *proofProducer.ProofWithHeader,
) (*encoding.BlockEvidence, error) {
	var (
		blockID = proofWithHeader.BlockID
		header  = proofWithHeader.Header
		proof   = proofWithHeader.Proof
	)

	log.Info(
		"Create new evidence",
		"blockID", blockID,
		"parentHash", proofWithHeader.Header.ParentHash,
		"hash", proofWithHeader.Header.Hash(),
		"signalRoot", proofWithHeader.Opts.SignalRoot,
		"tier", proofWithHeader.Tier,
	)

	// Get the corresponding L2 block.
	block, err := b.rpc.L2.BlockByHash(ctx, header.Hash())
	if err != nil {
		return nil, fmt.Errorf("failed to get L2 block with given hash %s: %w", header.Hash(), err)
	}

	if block.Transactions().Len() == 0 {
		return nil, fmt.Errorf("invalid block without anchor transaction, blockID %s", blockID)
	}

	// Validate TaikoL2.anchor transaction inside the L2 block.
	anchorTx := block.Transactions()[0]
	if err := b.anchorTxValidator.ValidateAnchorTx(ctx, anchorTx); err != nil {
		return nil, fmt.Errorf("invalid anchor transaction: %w", err)
	}

	// Get and validate this anchor transaction's receipt.
	if _, err = b.anchorTxValidator.GetAndValidateAnchorTxReceipt(ctx, anchorTx); err != nil {
		return nil, fmt.Errorf("failed to fetch anchor transaction receipt: %w", err)
	}

	evidence := &encoding.BlockEvidence{
		MetaHash:   proofWithHeader.Opts.MetaHash,
		ParentHash: proofWithHeader.Opts.ParentHash,
		BlockHash:  proofWithHeader.Opts.BlockHash,
		SignalRoot: proofWithHeader.Opts.SignalRoot,
		Graffiti:   b.graffiti,
		Tier:       proofWithHeader.Tier,
		Proof:      proof,
	}

	if proofWithHeader.Tier == encoding.TierSgxAndPseZkevmID {
		circuitsIdx, err := proofProducer.DegreeToCircuitsIdx(proofWithHeader.Degree)
		if err != nil {
			return nil, err
		}
		evidence.Proof = append(uint16ToBytes(circuitsIdx), evidence.Proof...)
	}

	return evidence, nil
}

// ForContest creates the evidence for contesting a L2 transition.
func (b *Builder) ForContest(
	ctx context.Context,
	header *types.Header,
	l2SignalService common.Address,
	event *bindings.TaikoL1ClientTransitionProved,
) (*encoding.BlockEvidence, error) {
	signalRoot, err := b.rpc.GetStorageRoot(ctx, b.rpc.L2GethClient, l2SignalService, event.BlockId)
	if err != nil {
		return nil, fmt.Errorf("failed to get L2 signal service storage root: %w", err)
	}
	blockInfo, err := b.rpc.TaikoL1.GetBlock(&bind.CallOpts{Context: ctx}, event.BlockId.Uint64())
	if err != nil {
		return nil, err
	}

	return &encoding.BlockEvidence{
		MetaHash:   blockInfo.MetaHash,
		ParentHash: event.ParentHash,
		BlockHash:  header.Hash(),
		SignalRoot: signalRoot,
		Tier:       event.Tier,
		Proof:      []byte{},
	}, nil
}

// uint16ToBytes converts an uint16 to bytes.
func uint16ToBytes(i uint16) []byte {
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, i)
	return b
}
