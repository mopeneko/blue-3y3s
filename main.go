package main

import (
	"context"
	"database/sql"
	"log"
	"os"
	"time"

	"./opprocessor"
	"github.com/apache/thrift/lib/go/thrift"
	"github.com/comail/colog"
	_ "github.com/go-sql-driver/mysql"
	"github.com/mopeneko/androidtoken"
	"github.com/mopeneko/lineapi"
	"github.com/mopeneko/linethrift"
)

func main() {
	startProgramTime := time.Now()
	db, _ := sql.Open("mysql", "tamaki:"+os.Getenv("MYSQL_PASSWORD")+"@tcp(10.25.96.4:3306)/tamaki?parseTime=true&loc=Asia%2FTokyo")
	defer db.Close()

	client, _ := getClient(db)
	ctx := context.Background()

	initLogger()

	opProcessor := opprocessor.Init(client, ctx, db, startProgramTime)
	go opProcessor.ClearKickedCount()
	opProcessor.Run()
}

func getClient(db *sql.DB) ([]*linethrift.TalkServiceClient, []*thrift.THttpClient) {
	rows, err := db.Query(`SELECT token FROM tokens`)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()
	client := []*linethrift.TalkServiceClient{}
	transport := []*thrift.THttpClient{}
	for rows.Next() {
		var token string
		if err := rows.Scan(&token); err != nil {
			log.Println("error:", err.Error())
		}
		authToken, err := androidtoken.CreateAuthToken(token)
		if err != nil {
			log.Fatalln("error:", err.Error())
		}
		cl, t, _ := lineapi.NewLineClient(authToken)
		client = append(client, cl)
		transport = append(transport, t)
	}
	return client, transport
}

func initLogger() {
	colog.SetDefaultLevel(colog.LDebug)
	colog.SetMinLevel(colog.LTrace)
	colog.SetFormatter(&colog.StdFormatter{
		Colors: true,
		Flag:   log.Ldate | log.Ltime | log.Lshortfile,
	})
	colog.Register()
}
