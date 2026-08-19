package view

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func unwrapSetOneofMessage(msg proto.Message) (proto.Message, bool) {
	if msg == nil {
		return nil, false
	}
	r := msg.ProtoReflect()
	oneofs := r.Descriptor().Oneofs()
	for i := 0; i < oneofs.Len(); i++ {
		field := r.WhichOneof(oneofs.Get(i))
		if field == nil || field.Kind() != protoreflect.MessageKind || field.IsList() || field.IsMap() {
			continue
		}
		inner := r.Get(field).Message().Interface()
		if pm, ok := inner.(proto.Message); ok {
			return pm, true
		}
	}
	return nil, false
}

func protoMessageToScalar(msg proto.Message) (any, bool) {
	if msg == nil {
		return nil, false
	}
	if scalar, ok := protoWrapperToScalar(msg); ok {
		return scalar, true
	}
	if s, ok := protoTemporalToString(msg); ok {
		return s, true
	}
	if inner, ok := unwrapSetOneofMessage(msg); ok {
		return protoMessageToScalar(inner)
	}
	return nil, false
}
