package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// maxAnswerBytes bounds what is read back. An answer is one small object, and a
// body larger than this is not one.
const maxAnswerBytes = 4 << 10

var _ auth.AuthorizeAsk = &httpAuthorizer{}

// httpAuthorizer puts the question to a URL, as a JSON post carrying the action.
type httpAuthorizer struct {
	client *http.Client
	url    string
	token  string
}

// encodeAction wraps the action in the canonical astral json envelope, the same
// one channel.JSONSender writes, so an authority decodes what it would decode
// off a channel and the two transports carry identical bytes.
func encodeAction(action auth.ActionObject) ([]byte, error) {
	envelope := astral.JSONAdapter{Type: action.ObjectType()}

	payload, err := json.Marshal(action)
	if err != nil {
		return nil, fmt.Errorf("encode action: %w", err)
	}
	envelope.Object = payload

	body, err := json.Marshal(&envelope)
	if err != nil {
		return nil, fmt.Errorf("encode envelope: %w", err)
	}

	return body, nil
}

// httpAnswer is what an authority answers with. The field is required: a body
// that does not carry it is an answer this code cannot read.
type httpAnswer struct {
	Allow *bool `json:"allow"`
}

// newHTTPAuthorizer builds the asker for an http:// or https:// endpoint. A token
// file is read here rather than at first use, so a missing one is a
// misconfiguration the operator sees at startup and not at the first question.
func newHTTPAuthorizer(config ExternalConfig) (*httpAuthorizer, error) {
	token := config.Token

	if config.TokenFile != "" {
		b, err := os.ReadFile(config.TokenFile)
		if err != nil {
			return nil, fmt.Errorf("read token file: %w", err)
		}
		token = strings.TrimSpace(string(b))
	}

	// note: the bound is the context's, applied per request by the caller. A
	// client timeout as well would cut the same wait twice, at two values.
	return &httpAuthorizer{client: &http.Client{}, url: config.Endpoint, token: token}, nil
}

func (a *httpAuthorizer) String() string { return a.url }

// Ask posts the action and reads the answer.
//
// why the answer is a body and not a status: a proxy answering 200 with its own
// error page would otherwise read as permission. A body that does not parse
// into the one field is an error, and an error refuses.
func (a *httpAuthorizer) Ask(ctx *astral.Context, action auth.ActionObject) (bool, error) {
	body, err := encodeAction(action)
	if err != nil {
		return false, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.url, bytes.NewReader(body))
	if err != nil {
		return false, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if a.token != "" {
		req.Header.Set("Authorization", "Bearer "+a.token)
	}

	res, err := a.client.Do(req)
	if err != nil {
		return false, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return false, fmt.Errorf("authority answered %v", res.Status)
	}

	var answer httpAnswer
	if err = json.NewDecoder(io.LimitReader(res.Body, maxAnswerBytes)).Decode(&answer); err != nil {
		return false, fmt.Errorf("decode answer: %w", err)
	}

	if answer.Allow == nil {
		return false, fmt.Errorf("answer carries no allow field")
	}

	return *answer.Allow, nil
}
