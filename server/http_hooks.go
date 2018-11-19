package main

import (
	"encoding/json"
	"net/http"

	"github.com/mattermost/mattermost-server/plugin"
)

// ServeHTTP allows the plugin to implement the http.Handler interface. Requests destined for the
// /plugins/{id} path will be routed to the plugin.
func (p *Plugin) ServeHTTP(c *plugin.Context, w http.ResponseWriter, r *http.Request) {
	var action *Action
	json.NewDecoder(r.Body).Decode(&action)

	if action == nil {
		encodeEphermalMessage(w, "Spin-Server Error: We could not decode the action")
		return
	}

	switch r.URL.Path {
	case "/destroy":
		info, err := p.deleteInstanceId(action.Context.UserID, action.Context.PublicDnsName)
		if err != nil {
			encodeEphermalMessage(w, err.Error())
			return
		}
		appErr := p.API.DeletePost(action.PostID)
		if appErr != nil {
			encodeEphermalMessage(w, appErr.Error())
			return
		}
		if info == "" {
			encodeEphermalMessage(w, "Nothing to destroy and Post removed.")
			return
		}
		encodeEphermalMessage(w, "Instance "+info+" destroyed.")
		return
	default:
		http.NotFound(w, r)
	}
}

func encodeEphermalMessage(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	payload := map[string]interface{}{
		"ephemeral_text": message,
	}

	json.NewEncoder(w).Encode(payload)
}
