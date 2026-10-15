package profile

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type AdminClient struct {
	baseURL        string
	serviceRoleKey string
	client         *http.Client
}

func NewAdminClient(projectURL, serviceRoleKey string, client *http.Client) *AdminClient {
	return &AdminClient{
		baseURL:        strings.TrimRight(projectURL, "/"),
		serviceRoleKey: serviceRoleKey,
		client:         client,
	}
}

func (c *AdminClient) DeleteUser(ctx context.Context, userID string) error {
	endpoint := c.baseURL + "/auth/v1/admin/users/" + url.PathEscape(userID) + "?should_soft_delete=false"
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return fmt.Errorf("delete user: %w", ErrAuthProvider)
	}
	req.Header.Set("apikey", c.serviceRoleKey)
	req.Header.Set("Authorization", "Bearer "+c.serviceRoleKey)
	res, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("delete user: %w", ErrAuthProvider)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("delete user: %w", ErrAuthProvider)
	}
	return nil
}
