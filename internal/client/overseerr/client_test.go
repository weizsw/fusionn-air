package overseerr

import "testing"

func TestGetSeasonRequestInfoSeparatesRequestStates(t *testing.T) {
	request := func(id int, status RequestStatus, is4K bool) Request {
		return Request{ID: id, Status: status, Is4K: is4K, Seasons: []SeasonReq{{SeasonNumber: 2}}}
	}
	tests := []struct {
		name  string
		media MediaInfo
		want  SeasonRequestState
	}{
		{"4K request only", MediaInfo{Requests: []Request{request(1, RequestStatusApproved, true)}}, SeasonRequestNone},
		{"failed", MediaInfo{Requests: []Request{request(2, RequestStatusFailed, false)}}, SeasonRequestFailed},
		{"declined", MediaInfo{Requests: []Request{request(3, RequestStatusDeclined, false)}}, SeasonRequestDeclined},
		{"active supersedes old decline", MediaInfo{Requests: []Request{request(3, RequestStatusDeclined, false), request(4, RequestStatusPending, false)}}, SeasonRequestActive},
		{"available supersedes failure", MediaInfo{Requests: []Request{request(2, RequestStatusFailed, false)}, Seasons: []SeasonInfo{{SeasonNumber: 2, Status: MediaStatusAvailable}}}, SeasonRequestAvailable},
		{"blocklisted show", MediaInfo{Status: MediaStatusBlocklisted}, SeasonRequestBlocklisted},
		{"deleted season", MediaInfo{Seasons: []SeasonInfo{{SeasonNumber: 2, Status: MediaStatusDeleted}}}, SeasonRequestDeleted},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := new(Client).GetSeasonRequestInfo(&TVDetails{MediaInfo: &test.media}, 2)
			if got.State != test.want {
				t.Fatalf("state = %v, want %v", got.State, test.want)
			}
		})
	}
}
