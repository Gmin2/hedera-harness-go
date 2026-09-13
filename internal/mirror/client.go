// Package mirror is a small typed client for the mirror node rest api.
// hh owns this instead of using sdk helpers because the sdk hardcodes
// localhost mirror rest to port 38081.
package mirror

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var ErrNotFound = errors.New("not found on mirror node")

type Client struct {
	base string // https://testnet.mirrornode.hedera.com/api/v1
	http *http.Client
}

func New(base string) *Client {
	return &Client{
		base: strings.TrimRight(base, "/"),
		http: &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) Base() string { return c.base }

// URL builds a full url for a path relative to /api/v1.
func (c *Client) URL(path string, q url.Values) string {
	u := c.base + "/" + strings.TrimLeft(path, "/")
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}

// origin is scheme://host, used to resolve links.next which is absolute from /api/v1.
func (c *Client) origin() string {
	u, err := url.Parse(c.base)
	if err != nil {
		return c.base
	}
	return u.Scheme + "://" + u.Host
}

// get fetches a url and decodes json into out. 404 returns ErrNotFound,
// 429 and 5xx are retried a couple of times.
func (c *Client) get(ctx context.Context, rawURL string, out any) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * 300 * time.Millisecond):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/json")
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		switch {
		case resp.StatusCode == http.StatusNotFound:
			return ErrNotFound
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			lastErr = fmt.Errorf("mirror %s: %s", rawURL, resp.Status)
			continue
		case resp.StatusCode >= 400:
			var e Error
			if json.Unmarshal(body, &e) == nil && len(e.Status.Messages) > 0 {
				return fmt.Errorf("mirror %s: %s", resp.Status, e.Status.Messages[0].Message)
			}
			return fmt.Errorf("mirror %s: %s", rawURL, resp.Status)
		}
		return json.Unmarshal(body, out)
	}
	return lastErr
}

// Get decodes one resource.
func (c *Client) Get(ctx context.Context, path string, q url.Values, out any) (string, error) {
	u := c.URL(path, q)
	return u, c.get(ctx, u, out)
}

// pages walks links.next until done. decode returns the next link.
func (c *Client) pages(ctx context.Context, first string, decode func(body json.RawMessage) (*string, error)) error {
	next := first
	for i := 0; next != "" && i < 50; i++ {
		var raw json.RawMessage
		if err := c.get(ctx, next, &raw); err != nil {
			return err
		}
		link, err := decode(raw)
		if err != nil {
			return err
		}
		if link == nil || *link == "" {
			return nil
		}
		next = c.origin() + *link
	}
	return nil
}

func (c *Client) Account(ctx context.Context, id string) (*Account, string, error) {
	var a Account
	u, err := c.Get(ctx, "accounts/"+id, url.Values{"transactions": {"false"}}, &a)
	if err != nil {
		return nil, u, err
	}
	return &a, u, nil
}

func (c *Client) AccountTokens(ctx context.Context, id, tokenID string) ([]TokenRelationship, string, error) {
	q := url.Values{"limit": {"100"}}
	if tokenID != "" {
		q.Set("token.id", tokenID)
	}
	u := c.URL("accounts/"+id+"/tokens", q)
	var out []TokenRelationship
	err := c.pages(ctx, u, func(body json.RawMessage) (*string, error) {
		var page TokenRelationships
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, err
		}
		out = append(out, page.Tokens...)
		return page.Links.Next, nil
	})
	return out, u, err
}

func (c *Client) Token(ctx context.Context, id string) (*Token, string, error) {
	var t Token
	u, err := c.Get(ctx, "tokens/"+id, nil, &t)
	if err != nil {
		return nil, u, err
	}
	return &t, u, nil
}

func (c *Client) Nft(ctx context.Context, tokenID string, serial int64) (*Nft, string, error) {
	var n Nft
	u, err := c.Get(ctx, "tokens/"+tokenID+"/nfts/"+strconv.FormatInt(serial, 10), nil, &n)
	if err != nil {
		return nil, u, err
	}
	return &n, u, nil
}

func (c *Client) Topic(ctx context.Context, id string) (*Topic, string, error) {
	var t Topic
	u, err := c.Get(ctx, "topics/"+id, nil, &t)
	if err != nil {
		return nil, u, err
	}
	return &t, u, nil
}

func (c *Client) TopicMessages(ctx context.Context, id string) ([]TopicMessage, string, error) {
	u := c.URL("topics/"+id+"/messages", url.Values{"limit": {"100"}, "order": {"asc"}})
	var out []TopicMessage
	err := c.pages(ctx, u, func(body json.RawMessage) (*string, error) {
		var page TopicMessages
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, err
		}
		out = append(out, page.Messages...)
		return page.Links.Next, nil
	})
	return out, u, err
}

func (c *Client) Schedule(ctx context.Context, id string) (*Schedule, string, error) {
	var s Schedule
	u, err := c.Get(ctx, "schedules/"+id, nil, &s)
	if err != nil {
		return nil, u, err
	}
	return &s, u, nil
}

// PendingAirdrops lists airdrops waiting for receiver to claim.
func (c *Client) PendingAirdrops(ctx context.Context, receiver, tokenID string) ([]TokenAirdrop, string, error) {
	return c.airdrops(ctx, "accounts/"+receiver+"/airdrops/pending", tokenID)
}

// OutstandingAirdrops lists airdrops sender has sent that are not claimed yet.
func (c *Client) OutstandingAirdrops(ctx context.Context, sender, tokenID string) ([]TokenAirdrop, string, error) {
	return c.airdrops(ctx, "accounts/"+sender+"/airdrops/outstanding", tokenID)
}

func (c *Client) airdrops(ctx context.Context, path, tokenID string) ([]TokenAirdrop, string, error) {
	q := url.Values{"limit": {"100"}}
	if tokenID != "" {
		q.Set("token.id", tokenID)
	}
	u := c.URL(path, q)
	var out []TokenAirdrop
	err := c.pages(ctx, u, func(body json.RawMessage) (*string, error) {
		var page TokenAirdrops
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, err
		}
		out = append(out, page.Airdrops...)
		return page.Links.Next, nil
	})
	return out, u, err
}

// Transaction looks up a transaction by sdk id and returns the parent entry
// (nonce 0), or the scheduled one when the id carries ?scheduled.
func (c *Client) Transaction(ctx context.Context, sdkID string) (*Transaction, string, error) {
	path, q, err := TxPath(sdkID)
	if err != nil {
		return nil, "", err
	}
	var list Transactions
	u, err := c.Get(ctx, "transactions/"+path, q, &list)
	if err != nil {
		return nil, u, err
	}
	if len(list.Transactions) == 0 {
		return nil, u, ErrNotFound
	}
	return &list.Transactions[0], u, nil
}

// ContractCall runs a read only call through the mirror node web3 api and
// returns the hex result. to and data are 0x prefixed.
func (c *Client) ContractCall(ctx context.Context, to, data string) (string, string, error) {
	u := c.URL("contracts/call", nil)
	body, _ := json.Marshal(map[string]any{"to": to, "data": data, "estimate": false, "block": "latest"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return "", u, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", u, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return "", u, ErrNotFound
	}
	if resp.StatusCode >= 400 {
		var e Error
		if json.Unmarshal(raw, &e) == nil && len(e.Status.Messages) > 0 {
			return "", u, fmt.Errorf("contract call %s: %s", resp.Status, e.Status.Messages[0].Message)
		}
		return "", u, fmt.Errorf("contract call: %s", resp.Status)
	}
	var out struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", u, err
	}
	return out.Result, u, nil
}
