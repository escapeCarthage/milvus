package proxy

import (
	"context"
	"errors"
	"log"
	"strconv"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/zilliztech/milvus-distributed/internal/msgstream"
	"github.com/zilliztech/milvus-distributed/internal/proto/commonpb"
	"github.com/zilliztech/milvus-distributed/internal/proto/internalpb"
	"github.com/zilliztech/milvus-distributed/internal/proto/schemapb"
	"github.com/zilliztech/milvus-distributed/internal/proto/servicepb"
)

const (
	reqTimeoutInterval = time.Second * 2
)

func (p *Proxy) Insert(ctx context.Context, in *servicepb.RowBatch) (*servicepb.IntegerRangeResponse, error) {
	it := &InsertTask{
		Condition: NewTaskCondition(ctx),
		BaseInsertTask: BaseInsertTask{
			BaseMsg: msgstream.BaseMsg{
				HashValues: in.HashKeys,
			},
			InsertRequest: internalpb.InsertRequest{
				MsgType:        internalpb.MsgType_kInsert,
				CollectionName: in.CollectionName,
				PartitionTag:   in.PartitionTag,
				RowData:        in.RowData,
			},
		},
		manipulationMsgStream: p.manipulationMsgStream,
	}

	var cancel func()
	it.ctx, cancel = context.WithTimeout(ctx, reqTimeoutInterval)
	// TODO: req_id, segment_id, channel_id, proxy_id, timestamps, row_ids

	defer cancel()

	fn := func() error {
		select {
		case <-ctx.Done():
			return errors.New("insert timeout")
		default:
			return p.taskSch.DmQueue.Enqueue(it)
		}
	}
	err := fn()

	if err != nil {
		return &servicepb.IntegerRangeResponse{
			Status: &commonpb.Status{
				ErrorCode: commonpb.ErrorCode_UNEXPECTED_ERROR,
				Reason:    err.Error(),
			},
		}, nil
	}

	err = it.WaitToFinish()
	if err != nil {
		return &servicepb.IntegerRangeResponse{
			Status: &commonpb.Status{
				ErrorCode: commonpb.ErrorCode_UNEXPECTED_ERROR,
				Reason:    err.Error(),
			},
		}, nil
	}

	return it.result, nil
}

func (p *Proxy) CreateCollection(ctx context.Context, req *schemapb.CollectionSchema) (*commonpb.Status, error) {
	cct := &CreateCollectionTask{
		Condition: NewTaskCondition(ctx),
		CreateCollectionRequest: internalpb.CreateCollectionRequest{
			MsgType: internalpb.MsgType_kCreateCollection,
			Schema:  &commonpb.Blob{},
			// TODO: req_id, timestamp, proxy_id
		},
		masterClient: p.masterClient,
	}
	schemaBytes, _ := proto.Marshal(req)
	cct.CreateCollectionRequest.Schema.Value = schemaBytes
	var cancel func()
	cct.ctx, cancel = context.WithTimeout(ctx, reqTimeoutInterval)
	defer cancel()

	fn := func() error {
		select {
		case <-ctx.Done():
			return errors.New("create collection timeout")
		default:
			return p.taskSch.DdQueue.Enqueue(cct)
		}
	}
	err := fn()
	if err != nil {
		return &commonpb.Status{
			ErrorCode: commonpb.ErrorCode_UNEXPECTED_ERROR,
			Reason:    err.Error(),
		}, err
	}

	err = cct.WaitToFinish()
	if err != nil {
		return &commonpb.Status{
			ErrorCode: commonpb.ErrorCode_UNEXPECTED_ERROR,
			Reason:    err.Error(),
		}, err
	}

	return cct.result, nil
}

func (p *Proxy) Search(ctx context.Context, req *servicepb.Query) (*servicepb.QueryResult, error) {
	qt := &QueryTask{
		Condition: NewTaskCondition(ctx),
		SearchRequest: internalpb.SearchRequest{
			MsgType: internalpb.MsgType_kSearch,
			Query:   &commonpb.Blob{},
			// TODO: req_id, proxy_id, timestamp, result_channel_id
		},
		queryMsgStream: p.queryMsgStream,
		resultBuf:      make(chan []*internalpb.SearchResult),
	}
	var cancel func()
	qt.ctx, cancel = context.WithTimeout(ctx, reqTimeoutInterval)
	// Hack with test, shit here but no other ways
	reqID, _ := strconv.Atoi(req.CollectionName[len(req.CollectionName)-1:])
	qt.ReqID = int64(reqID)
	queryBytes, _ := proto.Marshal(req)
	qt.SearchRequest.Query.Value = queryBytes
	log.Printf("grpc address of query task: %p", qt)
	defer cancel()

	fn := func() error {
		select {
		case <-ctx.Done():
			return errors.New("create collection timeout")
		default:
			return p.taskSch.DqQueue.Enqueue(qt)
		}
	}
	err := fn()
	if err != nil {
		return &servicepb.QueryResult{
			Status: &commonpb.Status{
				ErrorCode: commonpb.ErrorCode_UNEXPECTED_ERROR,
				Reason:    err.Error(),
			},
		}, nil
	}

	err = qt.WaitToFinish()
	if err != nil {
		return &servicepb.QueryResult{
			Status: &commonpb.Status{
				ErrorCode: commonpb.ErrorCode_UNEXPECTED_ERROR,
				Reason:    err.Error(),
			},
		}, nil
	}

	return qt.result, nil
}

func (p *Proxy) DropCollection(ctx context.Context, req *servicepb.CollectionName) (*commonpb.Status, error) {
	dct := &DropCollectionTask{
		Condition: NewTaskCondition(ctx),
		DropCollectionRequest: internalpb.DropCollectionRequest{
			MsgType: internalpb.MsgType_kDropCollection,
			// TODO: req_id, timestamp, proxy_id
			CollectionName: req,
		},
		masterClient: p.masterClient,
	}
	var cancel func()
	dct.ctx, cancel = context.WithTimeout(ctx, reqTimeoutInterval)
	defer cancel()

	fn := func() error {
		select {
		case <-ctx.Done():
			return errors.New("create collection timeout")
		default:
			return p.taskSch.DdQueue.Enqueue(dct)
		}
	}
	err := fn()
	if err != nil {
		return &commonpb.Status{
			ErrorCode: commonpb.ErrorCode_UNEXPECTED_ERROR,
			Reason:    err.Error(),
		}, err
	}

	err = dct.WaitToFinish()
	if err != nil {
		return &commonpb.Status{
			ErrorCode: commonpb.ErrorCode_UNEXPECTED_ERROR,
			Reason:    err.Error(),
		}, err
	}

	return dct.result, nil
}

func (p *Proxy) HasCollection(ctx context.Context, req *servicepb.CollectionName) (*servicepb.BoolResponse, error) {
	hct := &HasCollectionTask{
		Condition: NewTaskCondition(ctx),
		HasCollectionRequest: internalpb.HasCollectionRequest{
			MsgType: internalpb.MsgType_kHasCollection,
			// TODO: req_id, timestamp, proxy_id
			CollectionName: req,
		},
		masterClient: p.masterClient,
	}
	var cancel func()
	hct.ctx, cancel = context.WithTimeout(ctx, reqTimeoutInterval)
	defer cancel()

	fn := func() error {
		select {
		case <-ctx.Done():
			return errors.New("create collection timeout")
		default:
			return p.taskSch.DdQueue.Enqueue(hct)
		}
	}
	err := fn()
	if err != nil {
		return &servicepb.BoolResponse{
			Status: &commonpb.Status{
				ErrorCode: commonpb.ErrorCode_UNEXPECTED_ERROR,
				Reason:    err.Error(),
			},
		}, err
	}

	err = hct.WaitToFinish()
	if err != nil {
		return &servicepb.BoolResponse{
			Status: &commonpb.Status{
				ErrorCode: commonpb.ErrorCode_UNEXPECTED_ERROR,
				Reason:    err.Error(),
			},
		}, err
	}

	return hct.result, nil
}

func (p *Proxy) DescribeCollection(ctx context.Context, req *servicepb.CollectionName) (*servicepb.CollectionDescription, error) {
	dct := &DescribeCollectionTask{
		Condition: NewTaskCondition(ctx),
		DescribeCollectionRequest: internalpb.DescribeCollectionRequest{
			MsgType: internalpb.MsgType_kDescribeCollection,
			// TODO: req_id, timestamp, proxy_id
			CollectionName: req,
		},
		masterClient: p.masterClient,
	}
	var cancel func()
	dct.ctx, cancel = context.WithTimeout(ctx, reqTimeoutInterval)
	defer cancel()

	fn := func() error {
		select {
		case <-ctx.Done():
			return errors.New("create collection timeout")
		default:
			return p.taskSch.DdQueue.Enqueue(dct)
		}
	}
	err := fn()
	if err != nil {
		return &servicepb.CollectionDescription{
			Status: &commonpb.Status{
				ErrorCode: commonpb.ErrorCode_UNEXPECTED_ERROR,
				Reason:    err.Error(),
			},
		}, err
	}

	err = dct.WaitToFinish()
	if err != nil {
		return &servicepb.CollectionDescription{
			Status: &commonpb.Status{
				ErrorCode: commonpb.ErrorCode_UNEXPECTED_ERROR,
				Reason:    err.Error(),
			},
		}, err
	}

	return dct.result, nil
}

func (p *Proxy) ShowCollections(ctx context.Context, req *commonpb.Empty) (*servicepb.StringListResponse, error) {
	sct := &ShowCollectionsTask{
		Condition: NewTaskCondition(ctx),
		ShowCollectionRequest: internalpb.ShowCollectionRequest{
			MsgType: internalpb.MsgType_kDescribeCollection,
			// TODO: req_id, timestamp, proxy_id
		},
		masterClient: p.masterClient,
	}
	var cancel func()
	sct.ctx, cancel = context.WithTimeout(ctx, reqTimeoutInterval)
	defer cancel()

	fn := func() error {
		select {
		case <-ctx.Done():
			return errors.New("create collection timeout")
		default:
			return p.taskSch.DdQueue.Enqueue(sct)
		}
	}
	err := fn()
	if err != nil {
		return &servicepb.StringListResponse{
			Status: &commonpb.Status{
				ErrorCode: commonpb.ErrorCode_UNEXPECTED_ERROR,
				Reason:    err.Error(),
			},
		}, err
	}

	err = sct.WaitToFinish()
	if err != nil {
		return &servicepb.StringListResponse{
			Status: &commonpb.Status{
				ErrorCode: commonpb.ErrorCode_UNEXPECTED_ERROR,
				Reason:    err.Error(),
			},
		}, err
	}

	return sct.result, nil
}

func (p *Proxy) CreatePartition(ctx context.Context, in *servicepb.PartitionName) (*commonpb.Status, error) {
	return &commonpb.Status{
		ErrorCode: 0,
		Reason:    "",
	}, nil
}

func (p *Proxy) DropPartition(ctx context.Context, in *servicepb.PartitionName) (*commonpb.Status, error) {
	return &commonpb.Status{
		ErrorCode: 0,
		Reason:    "",
	}, nil
}

func (p *Proxy) HasPartition(ctx context.Context, in *servicepb.PartitionName) (*servicepb.BoolResponse, error) {
	return &servicepb.BoolResponse{
		Status: &commonpb.Status{
			ErrorCode: 0,
			Reason:    "",
		},
		Value: true,
	}, nil
}

func (p *Proxy) DescribePartition(ctx context.Context, in *servicepb.PartitionName) (*servicepb.PartitionDescription, error) {
	return &servicepb.PartitionDescription{
		Status: &commonpb.Status{
			ErrorCode: 0,
			Reason:    "",
		},
	}, nil
}

func (p *Proxy) ShowPartitions(ctx context.Context, req *servicepb.CollectionName) (*servicepb.StringListResponse, error) {
	return &servicepb.StringListResponse{
		Status: &commonpb.Status{
			ErrorCode: 0,
			Reason:    "",
		},
	}, nil
}
