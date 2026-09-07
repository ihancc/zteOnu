//go:build live

package query

import (
	"fmt"
	"testing"

	"github.com/septrum101/zteOnu/app/sso"
)

// TestLive_ForwardQuery hits the real 施工 App backend. Run with:
//
//	go test -tags=live ./app/query/ -run TestLive_ForwardQuery -v
//
// The user asked us to validate the flow end-to-end with tt_wangbang and
// 15838372919, so this test is opt-in via the `live` build tag.
func TestLive_ForwardQuery(t *testing.T) {
	tok, err := sso.New().Login("tt_wangbang")
	if err != nil {
		t.Fatalf("SSO login: %v", err)
	}
	t.Logf("token length: %d", len(tok))

	fc, err := NewForwardClient(tok)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := fc.QueryOne("15838372919")
	if err != nil {
		t.Fatalf("QueryOne: %v", err)
	}
	fmt.Printf("code=%d msg=%q\n", resp.Code, resp.Msg)
	if resp.Data != nil {
		fmt.Printf("  UserName=%q UserBand=%q UserNode=%q OrderStatus=%q\n",
			resp.Data.UserName, resp.Data.UserBand, resp.Data.UserNode, resp.Data.OrderStatus)
		fmt.Printf("  BindInfo=%q\n", resp.Data.BindInfo)
		fmt.Printf("  Create=%q Update=%q\n", resp.Data.CreateTime, resp.Data.UpdateTime)
	}
}
