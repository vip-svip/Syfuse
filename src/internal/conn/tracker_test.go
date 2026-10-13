package conn

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"
)

// fixedClock 是可控时钟：每次 Next 推进 step，测试用它保证时间序确定性。
type fixedClock struct {
	mu   sync.Mutex
	cur  time.Time
	step time.Duration
}

func newFixedClock(step time.Duration) *fixedClock {
	return &fixedClock{cur: time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC), step: step}
}

func (c *fixedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.cur
	c.cur = c.cur.Add(c.step)
	return t
}

func info(host string) ConnInfo {
	return ConnInfo{Protocol: "tcp", SrcAddr: "127.0.0.1:50000", DstHost: host, DstPort: 443,
		Labels: map[string]string{"node": "hk-01"}}
}

func TestOpenAssignsMonotonicIDsAndStartTime(t *testing.T) {
	clk := newFixedClock(time.Second)
	tr := NewTracker(TrackerOptions{Now: clk.Now})
	a := tr.Open(info("a.example.com"))
	b := tr.Open(info("b.example.com"))
	if a != 1 || b != 2 {
		t.Fatalf("ID 应从 1 单调递增，得到 %d / %d", a, b)
	}
	snap := tr.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("Snapshot 应有 2 条，得到 %d", len(snap))
	}
	if !snap[0].StartTime.Before(snap[1].StartTime) {
		t.Fatalf("开始时间应随分配序递增：%v >= %v", snap[0].StartTime, snap[1].StartTime)
	}
}

func TestOpenIgnoresCallerIDAndCopiesLabels(t *testing.T) {
	tr := NewTracker(TrackerOptions{})
	ci := info("a.example.com")
	ci.ID = 999
	ci.StartTime = time.Unix(0, 0)
	id := tr.Open(ci)
	if id == 999 {
		t.Fatal("调用方指定的 ID 必须被忽略")
	}
	ci.Labels["node"] = "篡改后"
	snap := tr.Snapshot()
	if snap[0].Labels["node"] != "hk-01" {
		t.Fatalf("Labels 应防御性拷贝，实际=%v", snap[0].Labels)
	}
}

func TestCloseAccountingAndIdempotency(t *testing.T) {
	tr := NewTracker(TrackerOptions{})
	id := tr.Open(info("a.example.com"))
	tr.Close(id, 100, 200, "client")
	tr.Close(id, 1, 1, "again") // 重复 Close 必须无效
	st := tr.Stats()
	if st.ActiveConnections != 0 || st.ClosedConnections != 1 || st.TotalConnections != 1 {
		t.Fatalf("计数错误：%+v", st)
	}
	if st.TotalBytesIn != 100 || st.TotalBytesOut != 200 {
		t.Fatalf("重复 Close 不应重复计字节：%+v", st)
	}
}

func TestCloseUnknownIDIsSafe(t *testing.T) {
	tr := NewTracker(TrackerOptions{})
	tr.Close(42, 1, 1, "x") // 从未 Open
	tr.Close(0, 1, 1, "x")  // ID 从 1 起
	if st := tr.Stats(); st.TotalConnections != 0 {
		t.Fatalf("未知 ID 不应产生记录：%+v", st)
	}
}

func TestStatsMatchesSnapshot(t *testing.T) {
	tr := NewTracker(TrackerOptions{})
	for i := 0; i < 5; i++ {
		id := tr.Open(info(fmt.Sprintf("h%d.example.com", i)))
		if i%2 == 0 {
			tr.Close(id, 10, 20, "client")
		}
	}
	st := tr.Stats()
	snap := tr.Snapshot()
	if st.TotalConnections != len(snap) {
		t.Fatalf("Stats.Total=%d 与 Snapshot=%d 不一致", st.TotalConnections, len(snap))
	}
	if st.ActiveConnections+st.ClosedConnections != st.TotalConnections {
		t.Fatalf("Active+Closed != Total：%+v", st)
	}
	var inSum, outSum int64
	for _, r := range snap {
		if r.Closed() {
			inSum += r.BytesIn
			outSum += r.BytesOut
		}
	}
	if inSum != st.TotalBytesIn || outSum != st.TotalBytesOut {
		t.Fatalf("字节聚合不一致：sum=(%d,%d) stats=(%d,%d)", inSum, outSum, st.TotalBytesIn, st.TotalBytesOut)
	}
}

func TestEvictionDropsOldestClosedOnly(t *testing.T) {
	clk := newFixedClock(time.Second)
	tr := NewTracker(TrackerOptions{MaxRecords: 3, Now: clk.Now})
	live := tr.Open(info("live.example.com"))
	closed1 := tr.Open(info("c1.example.com"))
	tr.Close(closed1, 1, 1, "client")
	closed2 := tr.Open(info("c2.example.com"))
	tr.Close(closed2, 1, 1, "client")
	closed3 := tr.Open(info("c3.example.com"))
	tr.Close(closed3, 1, 1, "client")
	// 容量 3：closed1 应被淘汰（最旧已关闭），live 必须保留
	snap := tr.Snapshot()
	if len(snap) != 3 {
		t.Fatalf("应淘汰到 3 条，实际 %d", len(snap))
	}
	for _, r := range snap {
		if r.ID == closed1 {
			t.Fatal("最旧的已关闭记录应被淘汰")
		}
		if r.ID == live && r.Closed() {
			t.Fatal("活动连接不应被淘汰")
		}
	}
}

func TestEvictionKeepsActiveWhenAllActive(t *testing.T) {
	tr := NewTracker(TrackerOptions{MaxRecords: 2})
	tr.Open(info("a.example.com"))
	tr.Open(info("b.example.com"))
	tr.Open(info("c.example.com")) // 全是活动连接，软上限被突破
	if got := len(tr.Snapshot()); got != 3 {
		t.Fatalf("活动连接永不淘汰，应保留 3 条，实际 %d", got)
	}
}

func TestRetentionLazyEviction(t *testing.T) {
	clk := newFixedClock(time.Hour)
	tr := NewTracker(TrackerOptions{Now: clk.Now, Retention: 2 * time.Hour})
	id := tr.Open(info("a.example.com"))
	tr.Close(id, 0, 0, "client")
	clk.mu.Lock()
	clk.cur = clk.cur.Add(3 * time.Hour)
	clk.mu.Unlock()
	tr.Open(info("b.example.com")) // 触发惰性淘汰
	snap := tr.Snapshot()
	if len(snap) != 1 || snap[0].DstHost != "b.example.com" {
		t.Fatalf("过期已关闭记录应被淘汰，实际 %v", snap)
	}
}

func TestSnapshotOrderingAndNonNilEmpty(t *testing.T) {
	tr := NewTracker(TrackerOptions{})
	empty := tr.Snapshot()
	if empty == nil {
		t.Fatal("空 Snapshot 必须返回非 nil")
	}
	// 注入非单调时钟：排序断言仍要求 (StartTime, ID) 升序
	tr2 := NewTracker(TrackerOptions{})
	ids := []uint64{tr2.Open(info("a")), tr2.Open(info("b")), tr2.Open(info("c"))}
	snap := tr2.Snapshot()
	for i, r := range snap {
		if r.ID != ids[i] {
			t.Fatalf("Snapshot 应按插入序返回：位置 %d 得到 ID %d，期望 %d", i, r.ID, ids[i])
		}
	}
}

func TestJSONFieldNamesAreFrozen(t *testing.T) {
	// 黄金值：字段名是 UDS 契约，改名会破坏 App。防漂移。
	clk := newFixedClock(time.Second)
	tr := NewTracker(TrackerOptions{Now: clk.Now})
	id := tr.Open(info("a.example.com"))
	tr.Close(id, 5, 6, "client")
	raw, err := json.Marshal(tr.Snapshot()[0])
	if err != nil {
		t.Fatal(err)
	}
	const want = `"id":1,"protocol":"tcp","srcAddr":"127.0.0.1:50000","dstHost":"a.example.com","dstPort":443,"startTime":`
	if !contains(string(raw), want) {
		t.Fatalf("JSON 字段名漂移：\n got: %s\nwant 前缀: %s", raw, want)
	}
	for _, key := range []string{`"endTime":`, `"bytesIn":5`, `"bytesOut":6`, `"closeReason":"client"`} {
		if !contains(string(raw), key) {
			t.Fatalf("JSON 缺字段 %s：%s", key, raw)
		}
	}
	stRaw, _ := json.Marshal(tr.Stats())
	for _, key := range []string{`"totalConnections":1`, `"activeConnections":0`, `"closedConnections":1`,
		`"totalBytesIn":5`, `"totalBytesOut":6`} {
		if !contains(string(stRaw), key) {
			t.Fatalf("Stats JSON 缺字段 %s：%s", key, stRaw)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

func TestConcurrentOpenClose1000(t *testing.T) {
	tr := NewTracker(TrackerOptions{})
	const n = 1000
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := tr.Open(info(fmt.Sprintf("h%d.example.com", i)))
			tr.Close(id, 1, 2, "client")
		}(i)
	}
	wg.Wait()
	st := tr.Stats()
	if st.TotalConnections != n || st.ClosedConnections != n || st.ActiveConnections != 0 {
		t.Fatalf("并发计数不精确：%+v", st)
	}
	if st.TotalBytesIn != n || st.TotalBytesOut != 2*n {
		t.Fatalf("并发字节聚合不精确：%+v", st)
	}
}
