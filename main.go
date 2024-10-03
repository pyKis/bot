package main

import (
	"database/sql"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

var db *sql.DB

func createTables() error {
	usersTable := `
    CREATE TABLE IF NOT EXISTS users (
        user_id BIGINT PRIMARY KEY,
        username TEXT,
        first_name TEXT,
        last_name TEXT,
        phone_number TEXT
    );`

	referralsTable := `
    CREATE TABLE IF NOT EXISTS referrals (
        referral_code VARCHAR(8) PRIMARY KEY,
        user_id BIGINT REFERENCES users(user_id)
    );`

	_, err := db.Exec(usersTable)
	if err != nil {
		return fmt.Errorf("ошибка при создании таблицы users: %v", err)
	}

	_, err = db.Exec(referralsTable)
	if err != nil {
		return fmt.Errorf("ошибка при создании таблицы referrals: %v", err)
	}

	return nil
}

func main() {
	envErr := godotenv.Load()
	if envErr != nil {
		log.Fatal("Ошибка загрузки файла .env")
	}

	// Получение значений переменных окружения
	botToken := os.Getenv("BOT_TOKEN")
	dbUser := os.Getenv("DB_USER")
	dbName := os.Getenv("DB_NAME")
	dbPassword := os.Getenv("DB_PASSWORD")
	dbHost := os.Getenv("DB_HOST")
	dbPort := os.Getenv("DB_PORT")
	dbSSLMode := os.Getenv("DB_SSLMODE")

	databaseURL := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		dbUser, dbPassword, dbHost, dbPort, dbName, dbSSLMode)

	var err error
	db, err = sql.Open("postgres", databaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	err = createTables()
	if err != nil {
		log.Fatal(err)
	}

	bot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = true

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message != nil {
			handleUpdate(bot, update)
		}
	}
}

func handleUpdate(bot *tgbotapi.BotAPI, update tgbotapi.Update) {
	if update.Message.IsCommand() {
		switch update.Message.Command() {
		case "start":
			handleStartCommand(bot, update)
		}
	} else if update.Message.Contact != nil {
		handleContact(bot, update)
	} else {
		switch update.Message.Text {
		case "поделиться контактом":
			requestContact(bot, update)
		case "сгенерировать ссылку":
			generateReferralLink(bot, update)
		}
	}
}

func handleStartCommand(bot *tgbotapi.BotAPI, update tgbotapi.Update) {
	user := update.Message.From
	saveUser(user)

	// Проверка на наличие реферального кода
	referralCode := update.Message.CommandArguments()
	if referralCode != "" {
		inviterID, inviterUsername, err := getInviterInfo(referralCode)
		if err == nil && inviterID != 0 {
			inviterName := inviterUsername
			if inviterName == "" {
				inviterName = fmt.Sprintf("пользователь с ID %d", inviterID)
			}

			// Отправка уведомления новому пользователю
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("Вас пригласил %s", inviterName))
			bot.Send(msg)
		}
	}

	msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Добро пожаловать! Выберите действие:")
	buttons := []tgbotapi.KeyboardButton{
		{Text: "поделиться контактом", RequestContact: true},
		{Text: "сгенерировать ссылку"},
	}
	keyboard := tgbotapi.NewReplyKeyboard(buttons)
	msg.ReplyMarkup = keyboard

	bot.Send(msg)
}

func getInviterInfo(referralCode string) (int64, string, error) {
	var inviterID int64
	var inviterUsername string

	// Поиск пригласившего пользователя по реферальному коду
	err := db.QueryRow(`
        SELECT u.user_id, u.username
        FROM referrals r
        JOIN users u ON r.user_id = u.user_id
        WHERE r.referral_code = $1
    `, referralCode).Scan(&inviterID, &inviterUsername)

	if err != nil {
		if err == sql.ErrNoRows {
			return 0, "", nil // реферальный код не найден
		}
		return 0, "", err
	}

	return inviterID, inviterUsername, nil
}

func handleContact(bot *tgbotapi.BotAPI, update tgbotapi.Update) {
	contact := update.Message.Contact
	_, err := db.Exec(`UPDATE users SET phone_number=$1 WHERE user_id=$2`, contact.PhoneNumber, update.Message.From.ID)
	if err != nil {
		log.Println("Ошибка сохранения контакта:", err)
	} else {
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Контакт успешно сохранён!")
		bot.Send(msg)
	}
}

func requestContact(bot *tgbotapi.BotAPI, update tgbotapi.Update) {
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Пожалуйста, поделитесь своим контактом, нажав на кнопку ниже.")
	button := tgbotapi.NewKeyboardButtonContact("поделиться контактом")
	keyboard := tgbotapi.NewReplyKeyboard([]tgbotapi.KeyboardButton{button})
	msg.ReplyMarkup = keyboard
	bot.Send(msg)
}

func generateReferralLink(bot *tgbotapi.BotAPI, update tgbotapi.Update) {
	referralCode := generateReferralCode()
	_, err := db.Exec(`INSERT INTO referrals (referral_code, user_id) VALUES ($1, $2) ON CONFLICT (referral_code) DO NOTHING`, referralCode, update.Message.From.ID)
	if err != nil {
		log.Println("Ошибка генерации реферальной ссылки:", err)
		return
	}

	referralLink := fmt.Sprintf("https://t.me/%s?start=%s", bot.Self.UserName, referralCode)
	msg := tgbotapi.NewMessage(update.Message.Chat.ID, fmt.Sprintf("Ваша реферальная ссылка: %s", referralLink))
	bot.Send(msg)
}

func generateReferralCode() string {
	rand.Seed(time.Now().UnixNano())
	letters := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	code := make([]rune, 8)
	for i := range code {
		code[i] = letters[rand.Intn(len(letters))]
	}
	return string(code)
}

func saveUser(user *tgbotapi.User) {
	_, err := db.Exec(`
        INSERT INTO users (user_id, username, first_name, last_name) 
        VALUES ($1, $2, $3, $4) 
        ON CONFLICT (user_id) DO NOTHING`,
		user.ID, user.UserName, user.FirstName, user.LastName,
	)
	if err != nil {
		log.Println("Ошибка сохранения пользователя:", err)
	}
}
