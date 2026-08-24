// Package tg — минимальный клиент Telegram Bot API: long polling, отправка
// сообщений и файлов, инлайн-кнопки, скачивание голосовых. Ровно те методы,
// которые нужны боту, без внешних зависимостей.
package tg

// Update — входящее обновление.
type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message"`
	EditedMessage *Message       `json:"edited_message"`
	CallbackQuery *CallbackQuery `json:"callback_query"`
}

// User — пользователь Telegram.
type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

// Chat — чат.
type Chat struct {
	ID int64 `json:"id"`
}

// Message — сообщение.
type Message struct {
	MessageID int64  `json:"message_id"`
	From      *User  `json:"from"`
	Chat      Chat   `json:"chat"`
	Date      int64  `json:"date"`
	Text      string `json:"text"`
	Caption   string `json:"caption"`
	Voice     *Voice `json:"voice"`
	Audio     *Voice `json:"audio"`
}

// Voice — голосовое сообщение (и аудиофайл: нужные поля совпадают).
type Voice struct {
	FileID   string `json:"file_id"`
	Duration int    `json:"duration"`
	MimeType string `json:"mime_type"`
	FileSize int64  `json:"file_size"`
}

// CallbackQuery — нажатие инлайн-кнопки.
type CallbackQuery struct {
	ID      string   `json:"id"`
	From    *User    `json:"from"`
	Message *Message `json:"message"`
	Data    string   `json:"data"`
}

// File — описание файла на серверах Telegram.
type File struct {
	FileID   string `json:"file_id"`
	FilePath string `json:"file_path"`
	FileSize int64  `json:"file_size"`
}

// InlineKeyboardButton — кнопка под сообщением.
type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
}

// InlineKeyboardMarkup — клавиатура под сообщением.
type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

// Keyboard собирает клавиатуру из одного ряда кнопок.
func Keyboard(buttons ...InlineKeyboardButton) *InlineKeyboardMarkup {
	return &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{buttons}}
}

// Button — сокращение для создания кнопки.
func Button(text, data string) InlineKeyboardButton {
	return InlineKeyboardButton{Text: text, CallbackData: data}
}
