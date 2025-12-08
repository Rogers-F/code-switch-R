package main

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/daodao97/xgo/xdb"
	_ "modernc.org/sqlite"
)

const timeLayout = "2006-01-02 15:04:05"

func init() {
	home, _ := os.UserHomeDir()
	const sqliteOptions = "?cache=shared&mode=rwc&_busy_timeout=5000&_journal_mode=WAL"

	if err := xdb.Inits([]xdb.Config{
		{
			Name:        "default",
			Driver:      "sqlite",
			DSN:         filepath.Join(home, ".code-switch", "app.db"+sqliteOptions),
			MaxOpenConn: 1,
			MaxIdleConn: 1,
		},
	}); err != nil {
		fmt.Printf("初始化 request_log 表失败: %v\n", err)
	}
}

func TestSeedMockRequestLogs(t *testing.T) {
	db, _ := xdb.DB("default")
	xdb.New("request_log").Delete()
	if err := SeedMockRequestLogs(16); err != nil {
		t.Fatalf("seed failed: %v", err)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM request_log").Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count == 0 {
		t.Fatal("no mock request_log rows inserted")
	}
	var minCreated, maxCreated string
	if err := db.QueryRow("SELECT MIN(created_at), MAX(created_at) FROM request_log").Scan(&minCreated, &maxCreated); err != nil {
		t.Fatalf("range query failed: %v", err)
	}
	t.Logf("mock request_log rows=%d (%s -> %s)", count, minCreated, maxCreated)
}

// SeedMockRequestLogs 生成模拟 request_log 数据，默认覆盖最近 3 个月。
func SeedMockRequestLogs(months int) error {
	model := xdb.New("request_log")
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	today := startOfDay(time.Now())
	totalDays := months * 30
	maxDaily := 18
	minDaily := 4
	platModels := map[string][]string{
		"claude": {
			"claude-sonnet-4-5-20250929",
			"claude-opus-4-1-20250805",
			"claude-sonnet-4-20250514",
			"claude-haiku-4-5-20251001",
			"claude-3-5-haiku-20241022",
		},
		"codex": {
			"gpt-5-codex",
			"gpt-5",
		},
	}
	providers := map[string][]string{
		"claude": {"kimi", "deepseek", "AICoding.sh"},
		"codex":  {"AICoding.sh"},
	}
	httpCodes := []int{200, 200, 200, 201, 400, 429, 500}
	timeBands := []struct {
		startHour int
		endHour   int
		weight    float64
	}{
		{0, 6, 0.5},
		{6, 12, 1.1},
		{12, 18, 1.35},
		{18, 24, 0.9},
	}
	weekdayBoost := map[time.Weekday]float64{
		time.Monday:    1.1,
		time.Tuesday:   1.15,
		time.Wednesday: 1.2,
		time.Thursday:  1.15,
		time.Friday:    1.05,
		time.Saturday:  0.85,
		time.Sunday:    0.8,
	}
	for dayOffset := 0; dayOffset < totalDays; dayOffset++ {
		currentDay := today.AddDate(0, 0, -dayOffset)
		progress := float64(dayOffset) / float64(totalDays)
		trendFactor := 0.35 + (1-progress)*0.9
		weekdayFactor := weekdayBoost[currentDay.Weekday()]
		variation := 0.7 + rng.Float64()*0.8
		activity := trendFactor * weekdayFactor * variation
		dailyTarget := int(math.Round(float64(minDaily) + activity*float64(maxDaily-minDaily)))
		if dailyTarget < len(timeBands) {
			dailyTarget = len(timeBands)
		}
		if rng.Float64() < 0.15 {
			dailyTarget += 4 + rng.Intn(6)
		}
		if rng.Float64() < 0.05 {
			dailyTarget += 8 + rng.Intn(12)
		}
		bandWeights := make([]float64, len(timeBands))
		for i, band := range timeBands {
			bandWeights[i] = band.weight
		}
		bandCounts := distributeCounts(dailyTarget, bandWeights, rng)
		for bandIdx, band := range timeBands {
			records := bandCounts[bandIdx]
			if records <= 0 {
				records = 1
			}
			for i := 0; i < records; i++ {
				platform := chooseRandomKey(rng, platModels)
				selectedModel := platModels[platform][rng.Intn(len(platModels[platform]))]
				provider := providers[platform][rng.Intn(len(providers[platform]))]
				httpCode := httpCodes[rng.Intn(len(httpCodes))]
				inputTokens := 300 + rng.Intn(6000)
				outputTokens := 150 + rng.Intn(2500)
				reasoningTokens := rng.Intn(500)
				cacheCreateTokens := int(float64(inputTokens) * (float64(rng.Intn(25)) / 100))
				cacheReadTokens := int(float64(outputTokens) * (float64(rng.Intn(15)) / 100))
				isStream := 0
				if rng.Intn(100) < 35 {
					isStream = 1
				}
				duration := 0.2 + rng.Float64()*8
				hourRange := band.endHour - band.startHour
				if hourRange <= 0 {
					hourRange = 1
				}
				hour := band.startHour + rng.Intn(hourRange)
				if hour >= 24 {
					hour = 23
				}
				minute := rng.Intn(60)
				timestamp := currentDay.Add(time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute)
				if _, err := model.Insert(xdb.Record{
					"platform":            platform,
					"model":               selectedModel,
					"provider":            provider,
					"http_code":           httpCode,
					"input_tokens":        inputTokens,
					"output_tokens":       outputTokens,
					"cache_create_tokens": cacheCreateTokens,
					"cache_read_tokens":   cacheReadTokens,
					"reasoning_tokens":    reasoningTokens,
					"is_stream":           isStream,
					"duration_sec":        duration,
					"created_at":          timestamp.Format(timeLayout),
				}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func chooseRandomKey(rng *rand.Rand, data map[string][]string) string {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	return keys[rng.Intn(len(keys))]
}

func distributeCounts(total int, weights []float64, rng *rand.Rand) []int {
	if total <= 0 {
		return make([]int, len(weights))
	}
	sum := 0.0
	for _, w := range weights {
		sum += w
	}
	if sum == 0 {
		sum = float64(len(weights))
		for i := range weights {
			weights[i] = 1
		}
	}
	counts := make([]int, len(weights))
	remaining := total
	for i, w := range weights {
		portion := int(math.Round((w / sum) * float64(total)))
		if portion < 1 {
			portion = 1
		}
		counts[i] = portion
		remaining -= portion
	}
	for remaining != 0 {
		index := rng.Intn(len(counts))
		if remaining > 0 {
			counts[index]++
			remaining--
		} else if counts[index] > 1 {
			counts[index]--
			remaining++
		}
	}
	return counts
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}
