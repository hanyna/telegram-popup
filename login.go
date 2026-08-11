//go:build telegram

package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// runLogin is the interactive console login (invoked via Login.bat with the
// -login flag). It reads phone/code/2FA from stdin and saves a session file
// the windowless main app then reuses. Kept fully separate from normal run.
func runLogin(dir string, cfg Config) {
	in := bufio.NewReader(os.Stdin)
	fmt.Println("==============================================")
	fmt.Println("  התחברות לטלגרם — נדרשת פעם אחת בלבד")
	fmt.Println("  (מאפשרת הורדת סרטונים ארוכים שחסומים אחרת)")
	fmt.Println("==============================================")
	fmt.Println()

	appID := cfg.TelegramAppID
	appHash := cfg.TelegramAppHash
	if appID == 0 || appHash == "" {
		fmt.Println("צריך api_id ו-api_hash מהאתר https://my.telegram.org")
		fmt.Println("(היכנס > API development tools > צור אפליקציה, והעתק את שני הערכים)")
		fmt.Println()
		fmt.Print("api_id: ")
		idLine, _ := in.ReadString('\n')
		fmt.Print("api_hash: ")
		hashLine, _ := in.ReadString('\n')
		fmt.Sscanf(strings.TrimSpace(idLine), "%d", &appID)
		appHash = strings.TrimSpace(hashLine)
		if appID != 0 && appHash != "" {
			// Persist so the main app can use the session afterwards.
			cfg.TelegramAppID = appID
			cfg.TelegramAppHash = appHash
			saveConfigTelegram(filepath.Join(dir, "config.json"), appID, appHash)
		}
	}
	if appID == 0 || appHash == "" {
		fmt.Println("\nחסרים api_id/api_hash. יוצא.")
		pauseConsole(in)
		return
	}

	tg := NewTGClient(appID, appHash, filepath.Join(dir, "telegram.session"), filepath.Join(dir, "cache"))
	if tg == nil {
		fmt.Println("שגיאה בהגדרת הלקוח.")
		pauseConsole(in)
		return
	}

	fmt.Print("\nמספר טלפון (בפורמט בינלאומי, למשל 972501234567): ")
	phoneLine, _ := in.ReadString('\n')
	phone := strings.TrimSpace(phoneLine)

	codeCb := func() (string, error) {
		fmt.Print("הזן את הקוד שקיבלת בטלגרם: ")
		line, err := in.ReadString('\n')
		return strings.TrimSpace(line), err
	}
	passwordCb := func() (string, error) {
		fmt.Print("סיסמת האימות הדו-שלבי (אם יש, אחרת Enter): ")
		line, err := in.ReadString('\n')
		return strings.TrimSpace(line), err
	}

	fmt.Println("\nמתחבר…")
	if err := tg.InteractiveLogin(phone, codeCb, passwordCb); err != nil {
		fmt.Printf("\nההתחברות נכשלה: %v\n", err)
		pauseConsole(in)
		return
	}
	fmt.Println("\n✅ ההתחברות הצליחה! מעכשיו סרטונים ארוכים יורדו אוטומטית.")
	fmt.Println("אפשר לסגור חלון זה ולהפעיל את התוכנה כרגיל (Start.bat).")
	pauseConsole(in)
}

func pauseConsole(in *bufio.Reader) {
	fmt.Println("\nלחץ Enter לסגירה…")
	_, _ = in.ReadString('\n')
}
