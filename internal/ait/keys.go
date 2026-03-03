package ait

import (
	"fmt"
	"sync"

	sqids "github.com/sqids/sqids-go"
)

const (
	publicIDMinLength = 5
)

var (
	sqidsOnce  sync.Once
	sqidsErr   error
	sqidsCodec *sqids.Sqids
)

func issueKeyCodec() (*sqids.Sqids, error) {
	sqidsOnce.Do(func() {
		sqidsCodec, sqidsErr = sqids.New(sqids.Options{
			MinLength: publicIDMinLength,
		})
	})

	return sqidsCodec, sqidsErr
}

func RootPublicID(prefix string, id int64) (string, error) {
	if id < 0 {
		return "", fmt.Errorf("internal issue ids must be non-negative")
	}

	codec, err := issueKeyCodec()
	if err != nil {
		return "", err
	}

	encoded, err := codec.Encode([]uint64{uint64(id)})
	if err != nil {
		return "", err
	}

	return prefix + "-" + encoded, nil
}
