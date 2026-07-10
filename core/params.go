package core

import (
	"errors"
	"fmt"
	"github.com/astralp2p/astral-go/astral"
	"strconv"
	"strings"
	"time"
)

var ErrKeyNotFound = errors.New("key not found")

type Params map[string]string

func (params Params) GetInt(key string) (int, error) {
	v, found := params[key]
	if !found {
		return 0, ErrKeyNotFound
	}

	i, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse error: %w", err)
	}

	return int(i), nil
}

func (params Params) SetInt(key string, value int) {
	params[key] = strconv.FormatInt(int64(value), 10)
}

func (params Params) GetUint64(key string) (uint64, error) {
	v, found := params[key]
	if !found {
		return 0, ErrKeyNotFound
	}

	i, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse error: %w", err)
	}

	return i, nil
}

func (params Params) GetNonce(key string) (astral.Nonce, error) {
	v, found := params[key]
	if !found {
		return 0, ErrKeyNotFound
	}

	if len(v) != 16 {
		return 0, errors.New("invalid nonce length")
	}

	i, err := strconv.ParseUint(v, 16, 64)
	if err != nil {
		return 0, fmt.Errorf("parse error: %w", err)
	}

	return astral.Nonce(i), nil
}

func (params Params) SetNonce(key string, n astral.Nonce) {
	s := strconv.FormatUint(uint64(n), 16)

	// add padding
	if len(s) < 16 {
		s = strings.Repeat("0", 16-len(s)) + s
	}

	params[key] = s
}

func (params Params) GetObjectID(key string) (*astral.ObjectID, error) {
	v, found := params[key]
	if !found {
		return nil, ErrKeyNotFound
	}

	id, err := astral.ParseID(v)
	if err != nil {
		return nil, err
	}

	return id, nil
}

func (params Params) GetIdentity(key string) (i *astral.Identity, err error) {
	v, found := params[key]
	if !found {
		return i, ErrKeyNotFound
	}

	return astral.ParseIdentity(v)
}

func (params Params) SetIdentity(key string, i *astral.Identity) {
	params[key] = i.String()
}

func (params Params) GetUnixNano(key string) (time.Time, error) {
	v, found := params[key]
	if !found {
		return time.Time{}, ErrKeyNotFound
	}

	nsec, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse error: %w", err)
	}

	return time.Unix(0, nsec), nil
}

func (params Params) SetUnixNano(key string, value time.Time) {
	params[key] = strconv.FormatInt(value.UnixNano(), 10)
}

func SplitQueryParams(query string) (path, params string) {
	if i := strings.IndexByte(query, '?'); i != -1 {
		return query[:i], query[i+1:]
	}
	return query, ""
}

// ParseParams parses &-separated key=value pairs; an item without '=' becomes a value under the empty key.
func ParseParams(params string) Params {
	var p = map[string]string{}

	var list = strings.Split(params, "&")
	for _, item := range list {
		var key, value string
		s := strings.SplitN(item, "=", 2)
		if len(s) == 2 {
			key, value = s[0], s[1]
		} else {
			value = s[0]
		}
		p[key] = value
	}

	return p
}

func ParseQuery(query string) (path string, params Params) {
	var s string
	path, s = SplitQueryParams(query)
	params = ParseParams(s)
	return
}

// Query builds a query string from path and params; param order is non-deterministic.
func Query(path string, params Params) string {
	var f = path
	var l []string
	for k, v := range params {
		l = append(l, k+"="+v)
	}
	if len(l) > 0 {
		f = f + "?" + strings.Join(l, "&")
	}

	return f
}
