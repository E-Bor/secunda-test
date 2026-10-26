package cache

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

const TTL = 5 * time.Minute

type Cache struct {
	client  *redis.Client
	breaker breaker
	logger  *slog.Logger
}

func New(address, password string, db int, logger *slog.Logger) *Cache {
	client := redis.NewClient(&redis.Options{
		Addr:                  address,
		Password:              password,
		DB:                    db,
		DialTimeout:           200 * time.Millisecond,
		ReadTimeout:           200 * time.Millisecond,
		WriteTimeout:          200 * time.Millisecond,
		PoolTimeout:           200 * time.Millisecond,
		MaxRetries:            -1,
		ContextTimeoutEnabled: true,
	})

	return &Cache{
		client:  client,
		breaker: breaker{threshold: 5, cooldown: 10 * time.Second},
		logger:  logger,
	}
}

func (c *Cache) Get(ctx context.Context, key string) ([]byte, bool) {
	if c == nil || ctx.Err() != nil {
		return nil, false
	}

	generation, ok := c.breaker.allow(time.Now())
	if !ok {
		return nil, false
	}

	ctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()

	data, err := c.client.Get(ctx, key).Bytes()
	c.record(generation, err)

	return data, err == nil
}

func (c *Cache) Set(ctx context.Context, key string, data []byte) {
	if c == nil || ctx.Err() != nil {
		return
	}

	generation, ok := c.breaker.allow(time.Now())
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()

	c.record(generation, c.client.Set(ctx, key, data, TTL).Err())
}

func (c *Cache) record(generation uint64, err error) {
	if c.breaker.finish(generation, err == nil || errors.Is(err, redis.Nil), time.Now()) {
		c.logger.Warn("Redis circuit opened", "retry_after", "10s", "error", err)
	}
}

func (c *Cache) Close() error {
	return c.client.Close()
}
