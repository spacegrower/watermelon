package codec

import (
	"encoding/json"

	"google.golang.org/grpc/encoding"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type JsonPB struct {
	name string
	protojson.MarshalOptions
	protojson.UnmarshalOptions
}

func (j JsonPB) Name() string {
	return j.name
}

func init() {
	encoding.RegisterCodec(NewJsonCodec())
}

func NewJsonCodec() *JsonPB {
	return &JsonPB{
		name:             "json",
		MarshalOptions:   protojson.MarshalOptions{},
		UnmarshalOptions: protojson.UnmarshalOptions{},
	}
}

func (j JsonPB) Marshal(v interface{}) (out []byte, err error) {
	if pm, ok := v.(proto.Message); ok {
		b, err := j.MarshalOptions.Marshal(pm)
		if err != nil {
			return nil, err
		}
		return b, nil
	}

	bts, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}

	return bts, nil
}

func (j JsonPB) Unmarshal(data []byte, v interface{}) (err error) {
	if pm, ok := v.(proto.Message); ok {
		if err := j.UnmarshalOptions.Unmarshal(data, pm); err != nil {
			return err
		}
		return nil
	}

	if err := json.Unmarshal(data, v); err != nil {
		return err
	}
	return nil
}
