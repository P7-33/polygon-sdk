package protocol

import (
	"context"
	"fmt"

	"github.com/0xPolygon/minimal/network/grpc"
	"github.com/0xPolygon/minimal/protocol/proto"
	"github.com/0xPolygon/minimal/types"
	"github.com/golang/protobuf/ptypes/any"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/hashicorp/go-hclog"
)

// serviceV1 is the GRPC server implementation for the v1 protocol
type serviceV1 struct {
	proto.UnimplementedV1Server

	syncer *Syncer
	logger hclog.Logger

	store blockchainShim
}

type rlpObject interface {
	MarshalRLPTo(dst []byte) []byte
	UnmarshalRLP(input []byte) error
}

func (s *serviceV1) Notify(ctx context.Context, req *proto.NotifyReq) (*empty.Empty, error) {
	id := ctx.(*grpc.Context).PeerID

	b := new(types.Block)
	if err := b.UnmarshalRLP(req.Raw.Value); err != nil {
		return nil, err
	}
	s.syncer.enqueueBlock(id, b)
	return &empty.Empty{}, nil
}

// GetCurrent implements the V1Server interface
func (s *serviceV1) GetCurrent(ctx context.Context, in *empty.Empty) (*proto.V1Status, error) {
	status := s.syncer.status.toProto()
	return status, nil
}

// GetObjectsByHash implements the V1Server interface
func (s *serviceV1) GetObjectsByHash(ctx context.Context, req *proto.HashRequest) (*proto.Response, error) {
	hashes, err := req.DecodeHashes()
	if err != nil {
		return nil, err
	}
	resp := &proto.Response{
		Objs: []*proto.Response_Component{},
	}
	for _, hash := range hashes {
		var obj rlpObject
		var found bool

		if req.Type == proto.HashRequest_BODIES {
			obj, found = s.store.GetBodyByHash(hash)
		} else if req.Type == proto.HashRequest_RECEIPTS {
			var raw []*types.Receipt
			raw, err = s.store.GetReceiptsByHash(hash)
			if err != nil {
				return nil, err
			}
			found = true

			receipts := types.Receipts(raw)
			obj = &receipts
		}

		var data []byte
		if found {
			data = obj.MarshalRLPTo(nil)
		} else {
			data = []byte{}
		}

		resp.Objs = append(resp.Objs, &proto.Response_Component{
			Spec: &any.Any{
				Value: data,
			},
		})
	}
	return resp, nil
}

const maxHeadersAmount = 190

// GetHeaders implements the V1Server interface
func (s *serviceV1) GetHeaders(ctx context.Context, req *proto.GetHeadersRequest) (*proto.Response, error) {
	if req.Number != 0 && req.Hash != "" {
		return nil, fmt.Errorf("cannot have both")
	}
	if req.Amount > maxHeadersAmount {
		req.Amount = maxHeadersAmount
	}

	var origin *types.Header
	var ok bool

	if req.Number != 0 {
		origin, ok = s.store.GetHeaderByNumber(uint64(req.Number))
	} else {
		var hash types.Hash
		if err := hash.UnmarshalText([]byte(req.Hash)); err != nil {
			return nil, err
		}
		origin, ok = s.store.GetHeaderByHash(hash)
	}

	if !ok {
		// return empty
		return &proto.Response{}, nil
	}

	skip := req.Skip + 1

	resp := &proto.Response{
		Objs: []*proto.Response_Component{},
	}
	addData := func(h *types.Header) {
		resp.Objs = append(resp.Objs, &proto.Response_Component{
			Spec: &any.Any{
				Value: h.MarshalRLPTo(nil),
			},
		})
	}

	// resp
	addData(origin)

	count := int64(1)
	for count < req.Amount {
		block := int64(origin.Number) + skip

		if block < 0 {
			break
		}
		origin, ok = s.store.GetHeaderByNumber(uint64(block))
		if !ok {
			break
		}
		count++

		// resp
		addData(origin)
	}

	return resp, nil
}

// Helper functions to decode responses from the grpc layer
func getBodies(ctx context.Context, clt proto.V1Client, hashes []types.Hash) ([]*types.Body, error) {
	input := []string{}
	for _, h := range hashes {
		input = append(input, h.String())
	}
	resp, err := clt.GetObjectsByHash(ctx, &proto.HashRequest{Hash: input, Type: proto.HashRequest_BODIES})
	if err != nil {
		return nil, err
	}
	res := []*types.Body{}
	for _, obj := range resp.Objs {
		var body types.Body
		if obj.Spec.Value != nil {
			if err := body.UnmarshalRLP(obj.Spec.Value); err != nil {
				return nil, err
			}
		}
		res = append(res, &body)
	}
	if len(res) != len(input) {
		return nil, fmt.Errorf("not correct size")
	}
	return res, nil
}
