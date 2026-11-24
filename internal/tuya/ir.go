package tuya

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

// Tuya IR Control API.
//
// Every shape below was confirmed against the live cloud after the project
// subscribed the IR Control service. Two details are easy to get wrong and each
// costs a request to discover:
//
//   - responses are JSON arrays (remotes, categories), not objects with a named
//     list field;
//   - the path address of a remote is its remote_id, not its remote_index.
//     Using the index answers 1106 "permission deny", which reads like an
//     authorization problem and is not one.
//
// Numbers arrive as JSON strings ("category_id":"5"), so they are parsed
// through FlexInt. A key is addressed by its `key` string: every `key_id` in a
// real code library is 0, so the numeric id carries no information.

// IRCategory is one remote-control category, e.g. air conditioner or TV.
type IRCategory struct {
	CategoryID   FlexInt `json:"category_id"`
	CategoryName string  `json:"category_name"`
}

// IRRemote is one remote control stored on a blaster.
type IRRemote struct {
	ID         string  `json:"remote_id"`
	Name       string  `json:"remote_name"`
	CategoryID FlexInt `json:"category_id"`
	// Index is the remote's index in the code library. It is informational:
	// paths use ID.
	Index     FlexInt `json:"remote_index"`
	BrandName string  `json:"brand_name"`

	// Category and Online are filled only for remotes discovered as blaster
	// sub-devices (learned remotes: RF switches and paired appliances), which
	// the /remotes list does not include.
	Category string `json:"category"`
	Online   *bool  `json:"online"`
}

// IsSubDevice reports whether the remote came from the sub-device listing.
func (r IRRemote) IsSubDevice() bool { return r.Category != "" }

// Online reports the remote's reachability. Library remotes carry no status
// and are driven through the blaster, so they are treated as available.
func (r IRRemote) Available() bool {
	return r.Online == nil || *r.Online
}

// IRKey is one button of a remote control.
type IRKey struct {
	Key  string `json:"key"`
	ID   int    `json:"key_id"`
	Name string `json:"key_name"`
	// Standard marks a key from the code library rather than a learned one.
	Standard bool `json:"standard_key"`
	// Code is the learned RF/IR payload of a non-standard key, as returned by
	// the learning-codes endpoint. An RF switch has no code library entry: the
	// key exists only because somebody pressed the physical remote, and the
	// payload is what actually has to be transmitted. Without it the cloud
	// answers 30706 "command or value not support".
	Code string `json:"-"`
}

// IRLearningCode is one learned payload stored on a remote.
type IRLearningCode struct {
	Code     string `json:"code"`
	ID       int    `json:"id"`
	KeyName  string `json:"key_name"`
	Name     string `json:"name"`
	RemoteID string `json:"remote_id"`
}

// IRKeys is the answer to a key-list request: the cloud wraps the list in an
// object carrying metadata about the remote.
type IRKeys struct {
	Keys        []IRKey `json:"key_list"`
	CategoryID  FlexInt `json:"category_id"`
	BrandID     FlexInt `json:"brand_id"`
	RemoteIndex FlexInt `json:"remote_index"`
}

// IsIRBlaster reports whether a device category is an infrared blaster, the
// hardware that owns remotes. RF switches and the remotes themselves live
// *under* a blaster and are not blasters.
func IsIRBlaster(category string) bool {
	switch strings.ToLower(category) {
	case "wnykq", "ir", "infrared":
		return true
	}
	return false
}

// Blasters returns the device ids of the infrared blasters in the home. A
// blaster owns every remote, so the IR provider needs them before it can list
// anything.
func (c *Client) Blasters(ctx context.Context) ([]string, error) {
	devices, err := c.ListDevices(ctx, nil)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, d := range devices {
		if IsIRBlaster(d.Category) {
			out = append(out, d.ID)
		}
	}
	return out, nil
}

// IRCategories lists the remote-control categories a blaster supports.
func (c *Client) IRCategories(ctx context.Context, blaster string) ([]IRCategory, error) {
	var cats []IRCategory
	path := "/v1.0/infrareds/" + url.PathEscape(blaster) + "/categories"
	if err := c.call(ctx, http.MethodGet, path, nil, nil, &cats); err != nil {
		return nil, err
	}
	return cats, nil
}

// IRRemotes lists the remotes stored on a blaster.
func (c *Client) IRRemotes(ctx context.Context, blaster string) ([]IRRemote, error) {
	var remotes []IRRemote
	query := url.Values{"page_no": {"1"}, "page_size": {"100"}}
	path := "/v1.0/infrareds/" + url.PathEscape(blaster) + "/remotes"
	if err := c.call(ctx, http.MethodGet, path, query, nil, &remotes); err != nil {
		return nil, err
	}
	return remotes, nil
}

// IRSubDevices lists the blaster's own devices. A learned remote — every RF
// switch paired to the blaster, and appliances paired through it — is a
// sub-device, and is absent from the /remotes list, which only carries
// code-library remotes. Listing sub-devices is therefore the only way to see
// them.
func (c *Client) IRSubDevices(ctx context.Context, blaster string) ([]IRRemote, error) {
	// The sub-device listing uses device field names (id, name, category,
	// online), not remote ones, so it decodes into its own shape.
	var subs []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Category string `json:"category"`
		Online   *bool  `json:"online"`
	}
	path := "/v1.0/devices/" + url.PathEscape(blaster) + "/sub-devices"
	if err := c.call(ctx, http.MethodGet, path, nil, nil, &subs); err != nil {
		return nil, err
	}
	out := make([]IRRemote, 0, len(subs))
	for _, d := range subs {
		out = append(out, IRRemote{
			ID:       d.ID,
			Name:     d.Name,
			Category: d.Category,
			Online:   d.Online,
		})
	}
	return out, nil
}

// IRKeys lists the buttons of one remote control. remoteID is the remote's
// remote_id, which is also its device id in the home listing.
//
// Learned keys are enriched with their payload: the /keys answer for an RF
// switch carries only a name, and sending that name is refused by the cloud.
func (c *Client) IRKeys(ctx context.Context, blaster, remoteID string) ([]IRKey, error) {
	var res IRKeys
	path := "/v1.0/infrareds/" + url.PathEscape(blaster) + "/remotes/" + url.PathEscape(remoteID) + "/keys"
	if err := c.call(ctx, http.MethodGet, path, nil, nil, &res); err != nil {
		return nil, err
	}

	codes, err := c.IRLearningCodes(ctx, blaster, remoteID)
	if err != nil {
		// A library remote has no learned payloads and this call is not
		// meaningful for it; the keys themselves are still usable.
		return res.Keys, nil
	}
	byName := make(map[string]string, len(codes))
	for _, lc := range codes {
		if lc.Name != "" && lc.Code != "" {
			byName[lc.Name] = lc.Code
		}
	}
	for i := range res.Keys {
		if code, ok := byName[res.Keys[i].Key]; ok {
			res.Keys[i].Code = code
		}
	}
	return res.Keys, nil
}

// IRLearningCodes lists the payloads learned for a remote. A code-library
// remote answers with an empty list.
func (c *Client) IRLearningCodes(ctx context.Context, blaster, remoteID string) ([]IRLearningCode, error) {
	var codes []IRLearningCode
	path := "/v1.0/infrareds/" + url.PathEscape(blaster) + "/remotes/" + url.PathEscape(remoteID) + "/learning-codes"
	if err := c.call(ctx, http.MethodGet, path, nil, nil, &codes); err != nil {
		return nil, err
	}
	return codes, nil
}

// IRSendKey transmits one button of a remote control.
//
// There are two mechanisms and they are not interchangeable:
//
//   - a code-library key is named to the cloud (POST .../command {"key":...});
//   - a learned key carries its own payload and must be replayed
//     (POST .../learning-codes {"code":...}). Sending the name of a learned key
//     to /command answers 30706 "command or value not support", which is the
//     bug that made RF switches look accepted but do nothing.
func (c *Client) IRSendKey(ctx context.Context, blaster, remoteID string, key IRKey) error {
	base := "/v1.0/infrareds/" + url.PathEscape(blaster) + "/remotes/" + url.PathEscape(remoteID)
	if key.Code != "" {
		body := struct {
			Code string `json:"code"`
		}{Code: key.Code}
		return c.call(ctx, http.MethodPost, base+"/learning-codes", nil, body, nil)
	}
	body := struct {
		Key string `json:"key"`
	}{Key: key.Key}
	return c.call(ctx, http.MethodPost, base+"/command", nil, body, nil)
}
