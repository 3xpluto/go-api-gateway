package mw

import "time"

type JWKSStats struct {
	URL       string    `json:"url"`
	KeyCount  int       `json:"key_count"`
	FetchedAt time.Time `json:"fetched_at"`
}

func (j *JWKSValidator) Stats() JWKSStats {
	if j == nil {
		return JWKSStats{}
	}
	j.mu.RLock()
	defer j.mu.RUnlock()
	return JWKSStats{
		URL:       j.url,
		KeyCount:  len(j.keys),
		FetchedAt: j.fetchedAt,
	}
}
