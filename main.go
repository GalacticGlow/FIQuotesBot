package main

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/mr-linch/go-tg"
	"github.com/mr-linch/go-tg/tgb"
	_ "modernc.org/sqlite"
)

var STATES_MAP = make(map[int]string)
var DEFAULT = "DEFAULT"
var WAITING_FOR_NAME = "WAITING FOR NAME"
var WAITING_FOR_QUOTE = "WAITING FOR QUOTE"
var WAITING_FOR_DELETE_NAME = "WAITING FOR DELETE NAME"
var WAITING_FOR_DELETE_CHOICE = "WAITING FOR DELETE CHOICE"
var USER_PROFQUOTES = make(map[int][]string)
var USER_PROFNAMES = make(map[int][]string)

func createDb() (*sql.DB, error) {
	dbURL := os.Getenv("DATABASE_URL")
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatal("Error opening database:", err)
	}

	query := `CREATE TABLE IF NOT EXISTS quotes (
            prof_name TEXT NOT NULL,
            quote TEXT NOT NULL
        )`

	_, err = db.Exec(query)
	if err != nil {
		fmt.Println("Error creating table:", err)
		return nil, err
	}

	return db, nil
}

func saveQuote(ctx context.Context, db *sql.DB, profName string, quote string) error {
	query := `INSERT INTO quotes (prof_name, quote) VALUES ($1, $2)`
	_, err := db.ExecContext(ctx, query, strings.ToLower(profName), quote)
	if err != nil {
		return err
	}
	return nil
}

func getQuotes(ctx context.Context, db *sql.DB, prof_name string) ([]string, error) {
	query := `SELECT quote FROM quotes WHERE prof_name = $1`
	rows, err := db.QueryContext(ctx, query, prof_name)
	if err != nil {
		return nil, err
	}
	var quotes []string
	for rows.Next() {
		var quote string
		if err := rows.Scan(&quote); err != nil {
			return nil, err
		}
		quotes = append(quotes, quote)
	}
	return quotes, nil
}

func deleteQuote(ctx context.Context, db *sql.DB, quote string) error {
	query := "DELETE FROM quotes WHERE quote = $1"
	_, err := db.ExecContext(ctx, query, quote)
	if err != nil {
		return err
	}
	return nil
}

func updateProfNamesCache(db *sql.DB) ([]string, error) {
	query := "SELECT prof_name FROM quotes"
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var profNames []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		if !slices.Contains(profNames, name) {
			profNames = append(profNames, name)
		}
	}

	return profNames, nil
}

func showDatabase(db *sql.DB) {
	rows, err := db.Query("SELECT prof_name, quote FROM quotes")
	if err != nil {
		fmt.Println("Error reading from database:", err)
		return
	}
	defer rows.Close()

	fmt.Println("=== Quotes Database ===")
	for rows.Next() {
		var name string
		var quote string
		err := rows.Scan(&name, &quote)
		if err != nil {
			fmt.Println("Error scanning row:", err)
			continue
		}
		fmt.Printf("Professor: %s\nQuote: %s\n\n", name, quote)
	}
}

func main() {
	godotenv.Load(".env")
	superID, err := strconv.Atoi(os.Getenv("SUPERADMIN_ID"))
	if err != nil {
		log.Fatal("please set SUPERADMIN_ID env variable")
	}
	var ADMINS = []int{superID}

	token := os.Getenv("TOKEN")
	if token == "" {
		log.Fatal("please set TOKEN environment variable")
	}

	db, err := createDb()
	if err != nil {
		fmt.Println("DB not created")
		fmt.Println(err)
	}
	showDatabase(db)
	for _, admin := range ADMINS {
		STATES_MAP[admin] = DEFAULT
	}

	profNamesCache, err := updateProfNamesCache(db)
	if err != nil {
		fmt.Println("Error updating profNames cache")
		fmt.Println(err)
	}
	fmt.Println(profNamesCache)

	client := tg.New(token)
	router := tgb.NewRouter()

	router.Message(
		func(ctx context.Context, msg *tgb.MessageUpdate) error {
			return msg.Answer("Я був призваний у цей чат з єдиною ціллю - цитувати найбільш впливових, легендарних та смішних викладачів Факультету Інформатики! " +
				"Якщо імя такого викладача згадається в чаті, то цитата про нього/неї буде надіслана, якщо вона існує").DoVoid(ctx)
		},
		tgb.Command("start"))

	router.Message(
		func(ctx context.Context, msg *tgb.MessageUpdate) error {
			return msg.Answer("Я був створений щоб зробити цей чат веселішим, надсилаючи найсмішніші цитати викладачів ФІ. Мій розробник - @StarryLuminescence").DoVoid(ctx)
		},
		tgb.Command("info"))

	router.Message(
		func(ctx context.Context, msg *tgb.MessageUpdate) error {
			userID := int(msg.From.ID)
			if slices.Contains(ADMINS, userID) && msg.Chat.Type == tg.ChatTypePrivate {
				STATES_MAP[userID] = WAITING_FOR_NAME
				return msg.Answer("Нова цитата підійшла? А який викладач її придумав?").DoVoid(ctx)
			}
			return nil
		},
		tgb.Command("addquote"))

	router.Message(
		func(ctx context.Context, msg *tgb.MessageUpdate) error {
			userID := int(msg.From.ID)
			if slices.Contains(ADMINS, userID) && msg.Chat.Type == tg.ChatTypePrivate {
				STATES_MAP[userID] = WAITING_FOR_DELETE_NAME
				return msg.Answer("Треба видалити цитату? А який викладач її придумав?").DoVoid(ctx)
			}
			return nil
		},
		tgb.Command("deletequote"))

	router.Message(
		func(ctx context.Context, msg *tgb.MessageUpdate) error {
			return msg.AnswerDice("\U0001F3B0").DoVoid(ctx)
		},
		tgb.Command("gmbl"))

	router.Message(
		func(ctx context.Context, msg *tgb.MessageUpdate) error {
			userID := int(msg.From.ID)
			fmt.Println(STATES_MAP[userID])
			if slices.Contains(ADMINS, userID) && msg.Chat.Type == tg.ChatTypePrivate && STATES_MAP[userID] != DEFAULT {
				if STATES_MAP[userID] == WAITING_FOR_NAME { //admin just pressed the addquotes command
					USER_PROFNAMES[userID] = strings.Split(msg.Text, " ")
					STATES_MAP[userID] = WAITING_FOR_QUOTE
					return msg.Answer("Окей, імя є, а тепер скиньте мені саму цитату:").DoVoid(ctx)
				} else if STATES_MAP[userID] == WAITING_FOR_QUOTE { //sent the professor name, waiting for quote
					quote := msg.Text
					profNames := USER_PROFNAMES[userID]
					for _, name := range profNames {
						err := saveQuote(ctx, db, name, quote)
						if err != nil {
							return msg.Answer("На жаль цитату не вийшло додати( Напишіть моему розробнику @StarryLuminescence").DoVoid(ctx)
						}
					}
					if err != nil {
						return msg.Answer("На жаль цитату не вийшло додати( Напишіть моему розробнику @StarryLuminescence").DoVoid(ctx)
					}
					profNamesCache, err = updateProfNamesCache(db)
					if err != nil {
						return msg.Answer("На жаль цитату не вийшло додати( Напишіть моему розробнику @StarryLuminescence").DoVoid(ctx)
					}
					STATES_MAP[userID] = DEFAULT
					return msg.Answer("Цитату успішно додав!").DoVoid(ctx)
				} else if STATES_MAP[userID] == WAITING_FOR_DELETE_NAME {
					profNameToDelete := msg.Text
					profQuotes, err := getQuotes(ctx, db, profNameToDelete)
					if err != nil {
						return msg.Answer("Щось я зламався...(( Напишіть моему розробнику @StarryLuminescence").DoVoid(ctx)
					}
					if len(profQuotes) == 0 {
						return msg.Answer("Не знаю я такого викладача... Ви точно ввели його імя правильно? Спробуйте-но ще раз:").DoVoid(ctx)
					}
					allQuotesStr := fmt.Sprintf("Ось всі цитати викладача %s, по порядку.\n Напишіть номер цитати яку ви бажаете видалити: \n", profNameToDelete)
					for i, quote := range profQuotes {
						allQuotesStr += fmt.Sprintf("%d", i+1) + "\n"
						allQuotesStr += quote + "\n" + "\n"
					}
					STATES_MAP[userID] = WAITING_FOR_DELETE_CHOICE
					USER_PROFQUOTES[userID] = profQuotes
					return msg.Answer(allQuotesStr).DoVoid(ctx)
				} else if STATES_MAP[userID] == WAITING_FOR_DELETE_CHOICE {
					quoteNum, err := strconv.Atoi(msg.Text)
					profQuotes := USER_PROFQUOTES[userID]
					if err != nil {
						return msg.Answer("Та то не число, скажіть який номер бажаєте:").DoVoid(ctx)
					} else if quoteNum > len(profQuotes) {
						return msg.Answer("Та немає там стільки цитат, скажіть нормальний номер))").DoVoid(ctx)
					}
					err = deleteQuote(ctx, db, profQuotes[quoteNum-1])
					if err != nil {
						return msg.Answer("Щось я зламався...(( Напишіть моему розробнику @StarryLuminescence").DoVoid(ctx)
					}
					STATES_MAP[userID] = DEFAULT
					return msg.Answer("Цитата успішно видалив!").DoVoid(ctx)
				}
			} else {
				message := strings.ToLower(msg.Text)
				for _, name := range profNamesCache {
					if strings.Contains(message, name) {
						quotes, err := getQuotes(ctx, db, name)
						if err != nil {
							return msg.Answer("Щось я зламався...(( Напишіть моему розробнику @StarryLuminescence").DoVoid(ctx)
						}
						if len(quotes) == 0 {
							return nil
						}
						quoteAnswer := quotes[rand.IntN(len(quotes))]
						if err == nil {
							return msg.Answer(quoteAnswer).
								ReplyParameters(tg.ReplyParameters{
									MessageID: msg.ID,
								}).
								DoVoid(ctx)
						} else {
							return msg.Answer("Щось я зламався...(( Напишіть моему розробнику @StarryLuminescence").DoVoid(ctx)
						}
					}
				}
			}
			return nil
		})

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	poller := tgb.NewPoller(router, client)

	log.Println("Bot is running... Press Ctrl+C to stop.")
	if err := poller.Run(ctx); err != nil {
		log.Fatal(err)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	go func() {
		log.Fatal(http.ListenAndServe(":"+port, nil))
	}()
}
