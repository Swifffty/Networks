package asynchttp

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/lesismal/nbio/logging"
	"github.com/lesismal/nbio/nbhttp"
)

const (
	userAgent   = "PlacesLab"
	maxBodySize = 5 << 20
)

type Client struct {
	engine *nbhttp.Engine
	nb     *nbhttp.Client
}

func New(timeout time.Duration) (*Client, error) {
	logging.SetLevel(logging.LevelNone)

	engine := nbhttp.NewEngine(nbhttp.Config{MaxHTTPBodySize: maxBodySize})
	if err := engine.Start(); err != nil {
		return nil, fmt.Errorf("asynchttp: запуск цикла событий: %w", err)
	}

	return &Client{
		engine: engine,
		nb: &nbhttp.Client{
			Engine:  engine,
			Timeout: timeout,
		},
	}, nil
}

func (c *Client) Close() {
	c.nb.Close()
	c.engine.Stop()
}

func (c *Client) Get(ctx context.Context, rawURL string, onDone func(status int, body []byte, err error)) {
	if err := ctx.Err(); err != nil {
		onDone(0, nil, err)
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		onDone(0, nil, fmt.Errorf("asynchttp: создание запроса: %w", err))
		return
	}
	req.Header.Set("User-Agent", userAgent)

	c.nb.Do(req, func(res *http.Response, _ net.Conn, err error) {
		if err != nil {
			onDone(0, nil, fmt.Errorf("asynchttp: запрос: %w", err))
			return
		}
		if res.ContentLength > maxBodySize {
			if res.Body != nil {
				res.Body.Close()
			}
			onDone(0, nil, nbhttp.ErrTooLong)
			return
		}
		var body []byte
		if res.Body != nil {
			body, err = io.ReadAll(io.LimitReader(res.Body, maxBodySize+1))

			res.Body.Close()
			if err != nil {
				onDone(0, nil, fmt.Errorf("asynchttp: чтение ответа: %w", err))
				return
			}
			if len(body) > maxBodySize {
				onDone(0, nil, nbhttp.ErrTooLong)
				return
			}
		}
		if err := ctx.Err(); err != nil {
			onDone(0, nil, err)
			return
		}
		onDone(res.StatusCode, body, nil)
	})
}
