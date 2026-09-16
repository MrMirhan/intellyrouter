package guided

import "testing"

const (
	imgTextBody       = `{"messages":[{"role":"user","content":"describe the image"},{"role":"assistant","content":"ok"},{"role":"user","content":[{"type":"image","source":{"type":"base64","data":"AAAA"}}]}]}`
	imgImageSpaceBody = `{"messages":[{"role":"user","content":"describe the image"},{"role":"assistant","content":"ok"},{"role":"user","content":[{"type": "image", "source": {"type": "base64", "data": "AAAA"}}]}]}`
	docBody           = `{"messages":[{"role":"user","content":[{"type":"document","source":{"type":"base64","data":"AAAA"}}]}]}`
	textBody          = `{"messages":[{"role":"user","content":"plain text only"}]}`
	// A tool result whose body text contains the literal substring "type":"image".
	// The cheap bytes.Contains check would have flagged this; the confirm pass
	// must not.
	toolResultBody = `{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"r1","content":"the literal word image is fine, no type:image block here"}]}]}`
)

func TestHasImage(t *testing.T) {
	cases := map[string]struct {
		body string
		want bool
	}{
		"text only":                       {textBody, false},
		"image block":                     {imgTextBody, true},
		"image block with spaces in type": {imgImageSpaceBody, true},
		"document block":                  {docBody, true},
		"tool result text":                {toolResultBody, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := HasImage([]byte(c.body)); got != c.want {
				t.Fatalf("HasImage(%s) = %v, want %v", name, got, c.want)
			}
		})
	}
}
