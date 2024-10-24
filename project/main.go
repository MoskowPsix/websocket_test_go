package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

// Определяем структуру для клиента
type ClientSock struct {
	id   string
	conn *websocket.Conn
	send chan []byte
	room *Room
}

// Определяем структуру для комнаты
type Room struct {
	clients    map[*ClientSock]bool
	register   chan *ClientSock
	unregister chan *ClientSock
	mu         sync.RWMutex
}

type MessageWeb struct {
	To      string `json:"to"`
	From    string `json:"from"`
	Message string `json:"message"`
}

// Создаем новую комнату
func newRoom() *Room {
	return &Room{
		clients:    make(map[*ClientSock]bool),
		register:   make(chan *ClientSock),
		unregister: make(chan *ClientSock),
	}
}

// Запускаем обработку клиентов в комнате
func (r *Room) run() {
	for {
		select {
		case client := <-r.register:
			r.mu.Lock()
			r.clients[client] = true
			r.mu.Unlock()
		case client := <-r.unregister:
			r.mu.Lock()
			if _, ok := r.clients[client]; ok {
				delete(r.clients, client)
				close(client.send)
			}
			r.mu.Unlock()
		}
	}
}

// Обработчик соединений WebSocket
func (c *ClientSock) read() {
	defer func() {
		c.room.unregister <- c
		c.conn.Close()
	}()

	for {
		var message MessageWeb
		_, msg, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
		err_str := json.Unmarshal(msg, &message)
		if err_str != nil {
			break
		}
		log.Printf("f%", message)
		switch message.Message {
		case "static":
			var clients []string
			clients, err = c.room.getAllClients()
			if err != nil {
				c.room.sendPrivateMessage(c.id, message.From, []byte(err.Error()))
			}
			c.room.sendPrivateMessage(c.id, message.From, []byte(strings.Join(clients, ",")))
		default:
			c.room.sendPrivateMessage(c.id, message.To, []byte(message.Message))
		}
	}
}

func (c *ClientSock) write() {
	defer c.conn.Close()
	for msg := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			break
		}
	}
}

// Отправляем приватное сообщение
func (r *Room) sendPrivateMessage(senderID, recipientID string, msg []byte) {
	var new_msg MessageWeb
	new_msg.To = senderID
	new_msg.From = recipientID
	new_msg.Message = string(msg)
	out, _ := json.Marshal(new_msg)
	r.mu.RLock()
	defer r.mu.RUnlock()
	for client := range r.clients {
		if client.id == recipientID {
			client.send <- []byte(out)
			break
		}
	}
}

func (r *Room) getAllClients() ([]string, error) {
	var clients_online []string
	r.mu.RLock()
	defer r.mu.RUnlock()
	for client := range r.clients {
		clients_online = append(clients_online, client.id)
	}
	if len(clients_online) == 0 {
		return nil, errors.New("empty clients")
	}
	return clients_online, nil
}

// Обработчик WebSocket соединений
func handleWebSocket(w http.ResponseWriter, r *http.Request, room *Room) {
	conn, err := websocket.Upgrade(w, r, nil, 1024, 1024)
	if err != nil {
		fmt.Println("Ошибка при обновлении соединения:", err)
		return
	}

	clientID := r.URL.Query().Get("id") // Получаем ID клиента из URL
	client := &ClientSock{conn: conn, id: clientID, send: make(chan []byte), room: room}

	room.register <- client

	go client.write()
	client.read()
}

func main() {
	room := newRoom()
	go room.run()
	http.HandleFunc("/", serveHTML)
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		handleWebSocket(w, r, room)
	})

	fmt.Println("WebSocket сервер запущен на :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		fmt.Println("Ошибка при запуске сервера:", err)
	}
}

func serveHTML(w http.ResponseWriter, r *http.Request) {
	// Определяем путь к HTML файлу
	htmlFilePath := filepath.Join("home.html")

	// Читаем содержимое HTML файла
	data, err := ioutil.ReadFile(htmlFilePath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Устанавливаем заголовок Content-Type
	w.Header().Set("Content-Type", "text/html")
	// Отправляем содержимое HTML файла
	w.Write(data)
}
