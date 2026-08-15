package cursorproto

import (
	"encoding/hex"
	"testing"
)

func fromHex(s string) []byte {
	b, _ := hex.DecodeString(s)
	return b
}

func TestParseMcpToolCallFixture(t *testing.T) {
	raw := fromHex("7a4a0a480a0b6765745f77656174686572120f0a046369747912071a0550617269731a28746f6f6c5f31343630313066612d616635612d343461332d613663642d6331386533616465316438")
	name, args := ParseMcpToolCall(raw)
	t.Logf("name=%q args=%s", name, args)
	if name != "get_weather" {
		t.Fatalf("name = %q", name)
	}
	if string(args) != `{"city":"Paris"}` {
		t.Fatalf("args = %s", args)
	}
}

func TestParseShellToolCallFixture(t *testing.T) {
	raw := fromHex("0ace010ab3010a126563686f20746f6f6c2d746573742d37383918b0ea012228746f6f6c5f63313538323539632d343931352d346564332d383137612d30323631336531643837302a046563686f423312310a046563686f12150a04776f7264120d746f6f6c2d746573742d3738391a126563686f20746f6f6c2d746573742d37383950c0b80268027080b899297a1652756e206563686f20746f6f6c2d746573742d373839880101aa010d70726f62652d636f6e766f2d311a1652756e206563686f20746f6f6c2d746573742d373839ca0328746f6f6c5f63313538323539632d343931352d346564332d383137612d3032363133653164383730d803f5da9bad8034")
	cmd, ok := ParseShellToolCall(raw)
	if !ok || cmd != "echo tool-test-789" {
		t.Fatalf("cmd=%q ok=%v", cmd, ok)
	}
}
