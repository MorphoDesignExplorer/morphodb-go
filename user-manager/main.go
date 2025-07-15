package main

import (
	"flag"
	"fmt"
	"net/mail"

	morphoroutes "github.com/MorphoDesignExplorer/morphodb-go/morpho-routes"
)

func main() {
	usage := `
	Usage: user-manager [OPTION] ...

	-create-admin-user	Create an admin user with an email and a password.
	-set-password		Reset a user's password.
	-get-user			Get a user's details.
	`

	// ./user-manager -create-admin-user email@example.com somethingsomething
	// ./user-manager -set-password email@example.com somethingsomething
	// ./user-manager -get-user email@example.com

	createAdminUser := flag.Bool("create-admin-user", false, "Create an admin user with an email, password.")
	setPassword := flag.Bool("set-password", false, "Reset a user's password.")
	getUser := flag.Bool("get-user", false, "Get a user's details.")
	help := flag.Bool("h", false, "")

	flag.Parse()

	if *help {
		fmt.Println(usage)
	}

	service, err := morphoroutes.StartService()
	if err != nil {
		fmt.Println(err)
		return
	}

	db, err := service.GetDB()
	if err != nil {
		fmt.Println(err)
		return
	}

	err = morphoroutes.InitAuthDB(db)
	if err != nil {
		fmt.Println(err)
		return
	}

	secrets := morphoroutes.Secrets{}
	err = secrets.Init()
	if err != nil {
		fmt.Println(err)
		return
	}

	if *createAdminUser {
		args := flag.Args()
		if len(args) != 2 {
			fmt.Println("Usage: user-manager -create-admin-user <EMAIL> <PASSWORD>")
			return
		}

		_, err = mail.ParseAddress(args[0])
		if err != nil {
			fmt.Println("<EMAIL> must be a valid email address.")
			fmt.Println("Usage: user-manager -create-admin-user <EMAIL> <PASSWORD>")
			return
		}

		if len(args[1]) < 7 {
			fmt.Println("<PASSWORD> must be at least 7 characters long.")
			fmt.Println("Usage: user-manager -create-admin-user <EMAIL> <PASSWORD>")
			return
		}

		err := morphoroutes.CreateUser(db, args[0], args[1], morphoroutes.Permissions{Create: true, Update: true, IsAdmin: true}, secrets)
		if err != nil {
			fmt.Println(err)
		}

	} else if *setPassword {
		args := flag.Args()
		if len(args) != 2 {
			fmt.Println("Usage: user-manager -set-password <EMAIL> <PASSWORD>")
			return
		}

		_, err = mail.ParseAddress(args[0])
		if err != nil {
			fmt.Println("<EMAIL> must be a valid email address.")
			fmt.Println("Usage: user-manager -set-password <EMAIL> <PASSWORD>")
			return
		}

		if len(args[1]) < 7 {
			fmt.Println("<PASSWORD> must be at least 7 characters long.")
			fmt.Println("Usage: user-manager -set-password <EMAIL> <PASSWORD>")
			return
		}

		user, err := morphoroutes.GetUser(db, args[0])
		if err != nil {
			fmt.Println(err)
			return
		}

		err = morphoroutes.ReplacePassword(db, user.Email, args[1], secrets)
		if err != nil {
			fmt.Println(err)
		}
	} else if *getUser {
		args := flag.Args()
		if len(args) != 1 {
			fmt.Println("Usage: user-manager -get-user <EMAIL>")
			return
		}

		user, err := morphoroutes.GetUser(db, args[0])
		if err != nil {
			fmt.Println(err)
			return
		}

		fmt.Println(user)
	} else {
		fmt.Println("no usage flag was provided.")
		fmt.Println(usage)
	}
}
