package notify

import (
	"encoding/json"
	"os"
	"strconv"
	"sync"
	"time"
)

// statePath guarda o id da ultima notificacao de cada usuario. Fica em /run
// (tmpfs): ids de notificacao nao sobrevivem a um reboot mesmo.
const statePath = "/run/open-turkey/notify.json"

// replaceWindow: uma notificacao mais nova que isso e atualizada no lugar em
// vez de empilhar outra (ex: o Steam reaberto varias vezes seguidas, ou o
// aviso de carencia virando o aviso de "fechado").
const replaceWindow = 90 * time.Second

type lastNotice struct {
	ID uint32    `json:"id"`
	At time.Time `json:"at"`
}

var stateMu sync.Mutex

// Show mostra uma notificacao do Omarchy ao usuario uid, substituindo a
// anterior dele se ela tiver menos de replaceWindow.
func Show(uid uint32, urgency, headline, body string) error {
	stateMu.Lock()
	defer stateMu.Unlock()

	state := map[string]lastNotice{}
	if data, err := os.ReadFile(statePath); err == nil {
		_ = json.Unmarshal(data, &state)
	}
	key := strconv.FormatUint(uint64(uid), 10)

	var replaceID uint32
	if last, ok := state[key]; ok && time.Since(last.At) < replaceWindow {
		replaceID = last.ID
	}
	id, err := Send(uid, replaceID, urgency, headline, body)
	if err != nil {
		return err
	}

	state[key] = lastNotice{ID: id, At: time.Now()}
	if err := os.MkdirAll("/run/open-turkey", 0755); err == nil {
		if data, err := json.Marshal(state); err == nil {
			_ = os.WriteFile(statePath, data, 0644)
		}
	}
	return nil
}
