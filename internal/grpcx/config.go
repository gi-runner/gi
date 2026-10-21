package grpcx

import (
	"errors"
	"fmt"

	"github.com/gi-runner/gi/internal/tlsconfig"
	"github.com/gi-runner/gi/internal/value"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

func transportCredentials(spec any) (credentials.TransportCredentials, error) {
	config, err := tlsconfig.Parse(spec)
	if err != nil {
		return nil, fmt.Errorf("grpc %w", err)
	}
	if config == nil {
		return insecure.NewCredentials(), nil
	}
	return credentials.NewTLS(config), nil
}

func requestMetadata(spec any) (metadata.MD, error) {
	if spec == nil {
		return metadata.MD{}, nil
	}
	fields, ok := value.AsMap(spec)
	if !ok {
		return nil, errors.New("grpc metadata expects an object")
	}
	md := metadata.MD{}
	for name, v := range fields {
		if v == nil {
			continue
		}
		if values, ok := value.AsSlice(v); ok {
			for _, item := range values {
				if item != nil {
					md.Append(name, value.Sprint(item))
				}
			}
		} else {
			md.Append(name, value.Sprint(v))
		}
	}
	return md, nil
}
