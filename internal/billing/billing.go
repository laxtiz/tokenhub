package billing

import "tokenhub/internal/db"

// Cost 按 tokens 与模型单价（每百万 token）计算费用。
// prompt 为完整输入（含缓存命中），缓存部分按缓存价计费、不重复收输入价。
func Cost(m *db.Model, prompt, completion, cacheRead, cacheWrite int64) float64 {
	uncached := prompt - cacheRead - cacheWrite
	if uncached < 0 {
		uncached = 0
	}
	return (float64(uncached)*m.InputPrice +
		float64(completion)*m.OutputPrice +
		float64(cacheRead)*m.CacheReadPrice +
		float64(cacheWrite)*m.CacheWritePrice) / 1_000_000
}
