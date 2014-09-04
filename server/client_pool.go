package server

import (
	"container/list"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"sync"
	"time"

	"warcluster/entities"
	"warcluster/server/response"
)

// Thread-safe pool of all clients, with opened sockets.
type ClientPool struct {
	mutex  sync.Mutex
	pool   map[string]*list.List
	ticker *time.Ticker
}

func NewClientPool() *ClientPool {
	cp := new(ClientPool)
	cp.pool = make(map[string]*list.List)
	cp.ticker = time.NewTicker(cfg.Server.Ticker * time.Millisecond)
	go cp.runStateChangeCycle()
	return cp
}

func (cp *ClientPool) runStateChangeCycle() {
	defer func() {
		if panicked := recover(); panicked != nil {
			log.Println(fmt.Sprintf(
				"%s\n\nState change cycle has panicked!:\n\n%s",
				panicked,
				debug.Stack(),
			))
			go cp.runStateChangeCycle()
		}
	}()
	for _ = range cp.ticker.C {
		for _, clients := range cp.pool {
			for element := clients.Front(); element != nil; element = element.Next() {
				element.Value.(*Client).sendStateChange()
			}
		}
	}
}

// Returns player's instance by username in order not to hit the database
func (cp *ClientPool) Player(username string) (*entities.Player, error) {
	if element, ok := cp.pool[username]; ok {
		return element.Front().Value.(*Client).Player, nil
	}
	return nil, errors.New("Player not logged in")
}

// Adds the given client to the pool.
func (cp *ClientPool) Add(client *Client) {
	cp.mutex.Lock()
	defer cp.mutex.Unlock()

	if _, ok := cp.pool[client.Player.Username]; !ok {
		cp.pool[client.Player.Username] = list.New()
	}
	element := cp.pool[client.Player.Username].PushBack(client)
	client.poolElement = element
}

// Remove the client to the pool.
// It is safe to remove non-existing client.
func (cp *ClientPool) Remove(client *Client) {
	cp.mutex.Lock()
	defer cp.mutex.Unlock()

	playerInPool, ok := cp.pool[client.Player.Username]
	if ok {
		playerInPool.Remove(client.poolElement)

		if playerInPool.Len() == 0 {
			delete(cp.pool, client.Player.Username)
		}
	}
}

// Broadcast sends the given message to every session in the pool.
func (cp *ClientPool) BroadcastToAll(response response.Responser) {
	for _, clients := range cp.pool {

		client := clients.Front().Value.(*Client)
		response.Sanitize(client.Player)
		cp.Send(client.Player, response)
	}
}

// Broadcasts state change of an entity to all interested parties
func (cp *ClientPool) Broadcast(entity entities.Entity) {
	defer func() {
		if panicked := recover(); panicked != nil {
			return
		}
	}()

	for _, clients := range cp.pool {
		for element := clients.Front(); element != nil; element = element.Next() {
			element.Value.(*Client).pushStateChange(entity)
		}
	}
}

func (cp *ClientPool) UpdateSpyReports(player *entities.Player) {
	defer func() {
		if panicked := recover(); panicked != nil {
			return
		}
	}()

	poolMember := cp.pool[player.Username]
	for element := poolMember.Front(); element != nil; element = element.Next() {
		client := element.Value.(*Client)
		client.Player.UpdateSpyReports()
	}
}

// Sanitizes given response and sends it to every player's session in the pool.
func (cp *ClientPool) Send(player *entities.Player, response response.Responser) {
	defer func() {
		if panicked := recover(); panicked != nil {
			return
		}
	}()
	response.Sanitize(player)

	message, _ := json.Marshal(response)

	for element := cp.pool[player.Username].Front(); element != nil; element = element.Next() {
		client := element.Value.(*Client)
		client.Session.Send(message)
	}
}
