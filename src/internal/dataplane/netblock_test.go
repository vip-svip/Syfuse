package dataplane

import (
	"testing"

	"github.com/vip-svip/syfuse/src/internal/outbound"
)

// NetBlock 钩子：封禁的 host 在路由决策**之前**被拒，
// 拒绝码与 Router.Reject 相同（ErrPolicyRejected），blocked 计数 +1。
// 域名与字面 IP 都能拦（ipcidr 规则源的连接层出口）。
func TestNetBlock(t *testing.T) {
	echo := newEchoUpstream(t, "BANNER")

	blocked := map[string]bool{"banned.example.com": true, "9.9.9.9": true}
	dp, err := New(Options{
		Listen:   "127.0.0.1:0",
		Proxy:    &outbound.DirectDialer{},
		NetBlock: func(host string) bool { return blocked[host] },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	addr, err := dp.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer dp.Close()

	// 未封禁 host 正常走通（目标是 echo 上游，直连可达）。
	c, _ := socks5Client(t, addr, echo.addr())
	c.Close()

	// 封禁域名被拒（入站回 0x02 policy rejected）。
	c2, _ := socks5Client(t, addr, "banned.example.com:80")
	c2.Close()

	// 封禁字面 IP 被拒。
	c3, _ := socks5Client(t, addr, "9.9.9.9:443")
	c3.Close()

	if got := dp.Stats().Blocked; got != 2 {
		t.Fatalf("Blocked = %d, want 2", got)
	}
	if dp.Stats().Connections != 1 {
		t.Fatalf("封禁连接不得进连接数: %+v", dp.Stats())
	}
}
