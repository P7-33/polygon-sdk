package dev

import (
	"context"
	"fmt"
	"time"

	"github.com/0xPolygon/minimal/blockchain"
	"github.com/0xPolygon/minimal/consensus"
	"github.com/0xPolygon/minimal/network"
	"github.com/0xPolygon/minimal/state"
	"github.com/0xPolygon/minimal/txpool"
	"github.com/0xPolygon/minimal/types"
	"github.com/hashicorp/go-hclog"
	"google.golang.org/grpc"
)

// Dev consensus protocol seals any new transaction inmediatly
type Dev struct {
	logger hclog.Logger

	notifyCh chan struct{}
	closeCh  chan struct{}

	txpool *txpool.TxPool

	blockchain *blockchain.Blockchain
	executor   *state.Executor
}

func Factory(ctx context.Context, config *consensus.Config, txpool *txpool.TxPool, network *network.Server, blockchain *blockchain.Blockchain, executor *state.Executor, srv *grpc.Server, logger hclog.Logger) (consensus.Consensus, error) {
	logger = logger.Named("dev")

	d := &Dev{
		logger:     logger,
		notifyCh:   make(chan struct{}),
		closeCh:    make(chan struct{}),
		blockchain: blockchain,
		executor:   executor,
		txpool:     txpool,
	}

	// enable dev mode so that we can accept non-signed txns
	txpool.EnableDev()
	txpool.NotifyCh = d.notifyCh

	return d, nil
}

func (d *Dev) StartSeal() {
	go d.run()
}

func (d *Dev) run() {
	d.logger.Info("started")

	for {
		// wait until there is a new txn
		select {
		case <-d.notifyCh:
		case <-d.closeCh:
			return
		}

		// pick txn from the pool and seal them
		header := d.blockchain.Header()
		d.do(header)
	}
}

func (d *Dev) do(parent *types.Header) error {
	num := parent.Number
	header := &types.Header{
		ParentHash: parent.Hash,
		Number:     num + 1,
		GasLimit:   100000000, // placeholder for now
		Timestamp:  uint64(time.Now().Unix()),
	}

	transition, err := d.executor.BeginTxn(parent.StateRoot, header)
	if err != nil {
		return err
	}

	for {
		txn, retFn := d.txpool.Pop()
		if txn == nil {
			break
		}
		if err := transition.Write(txn); err != nil {
			fmt.Println("-- err --")
			fmt.Println(err)

			retFn()
			break
		}
	}

	_, root := transition.Commit()

	header.StateRoot = root
	header.GasUsed = transition.TotalGas()

	// header hash is computed inside buildBlock
	block := consensus.BuildBlock(header, nil, transition.Receipts())

	if err := d.blockchain.WriteBlocks([]*types.Block{block}); err != nil {
		panic(err)
	}

	return nil
}

func (d *Dev) VerifyHeader(parent *types.Header, header *types.Header) error {
	// All blocks are valid
	return nil
}

func (d *Dev) Prepare(header *types.Header) error {
	// TODO: Remove
	return nil
}

func (d *Dev) Seal(block *types.Block, ctx context.Context) (*types.Block, error) {
	// TODO: Remove
	return nil, nil
}

func (d *Dev) Close() error {
	close(d.closeCh)
	return nil
}
