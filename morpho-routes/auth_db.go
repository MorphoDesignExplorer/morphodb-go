package morphoroutes

import (
	"crypto/sha512"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
)

type User struct {
	Email        string
	PasswordHash string
	Permissions  Permissions
}

func (p *Permissions) Scan(src any) error {
	if srcString, ok := src.(string); ok {
		err := json.Unmarshal([]byte(srcString), p)
		if err != nil {
			return fmt.Errorf("Could not scan column into Permissions: %s", err)
		}
		return nil
	} else {
		return fmt.Errorf("Could not scan column into Permissions. Column was not a string.")
	}
}

func InitAuthDB(db *sql.DB) error {
	_, err := db.Exec("CREATE TABLE IF NOT EXISTS user (email text primary key, password_hash text, permissions text);")
	if err != nil {
		return err
	}

	return nil
}

func GetUser(db *sql.DB, email string) (u User, err error) {
	row := db.QueryRow("SELECT email, password_hash, permissions FROM user WHERE email = ?", email)
	err = row.Scan(&u.Email, &u.PasswordHash, &u.Permissions)
	return
}

func hashPassword(password, salt []byte) string {
	password = append(password, salt...)
	h := sha512.New()
	h.Write(password)
	digest := h.Sum(nil)
	return base64.StdEncoding.EncodeToString(digest)
}

func VerifyUser(u User, password string, s Secrets) bool {
	newPasswordHash := hashPassword([]byte(password), s.password)
	return newPasswordHash == u.PasswordHash
}

func CreateUser(db *sql.DB, email string, password string, permissions Permissions, s Secrets) error {
	passHash := hashPassword([]byte(password), s.password)
	permBytes, err := json.Marshal(permissions)
	if err != nil {
		return err
	}

	_, err = db.Exec("INSERT INTO user(email, password_hash, permissions) VALUES(?,?,?)", email, passHash, string(permBytes))
	if err != nil {
		return err
	}

	return nil
}

/*
Replaces the password of a user with a new password in the database.

Returns nil if the operation is successful, else return an error.
*/
func ReplacePassword(db *sql.DB, email string, password string, s Secrets) error {
	newPasswordHash := hashPassword([]byte(password), s.password)
	result, err := db.Exec("UPDATE user SET password_hash = ? WHERE email = ?", newPasswordHash, email)
	if err != nil {
		return err
	} else if affected, err := result.RowsAffected(); affected == 0 || err != nil {
		if err != nil {
			return err
		} else if affected == 0 {
			return fmt.Errorf("%s's password was not updated. Maybe the user doesn't exist?", email)
		}
	}
	return nil
}
