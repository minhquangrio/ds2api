package geminiweb

func (c *Client) IsClosed() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.closed
}

func (c *Client) Proxy() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.proxy
}

func (c *Client) ProxyID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.proxyID
}
