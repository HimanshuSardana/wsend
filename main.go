package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mdp/qrterminal/v3"
)

var client *whatsmeow.Client

func eventHandler(evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		fmt.Printf("Received a message from %s: %s\n", v.Info.Chat, v.Message.GetConversation())
	case *events.LoggedOut:
		fmt.Println("Logged out, please re-scan the QR code")
		qrChan, _ := client.GetQRChannel(context.Background())
		err := client.Connect()
		if err != nil {
			fmt.Printf("Connection error: %v\n", err)
			return
		}
		for e := range qrChan {
			if e.Event == "code" {
				displayQRCode(e.Code)
			} else {
				fmt.Printf("Login event: %s\n", e.Event)
			}
		}
	}
}

func displayQRCode(code string) {
	config := qrterminal.Config{
		Level:      qrterminal.M,
		Writer:     os.Stdout,
		HalfBlocks: true,
	}
	qrterminal.GenerateWithConfig(code, config)
	fmt.Println("\nScan this QR code with WhatsApp on your phone")
	fmt.Println("Or press Ctrl+C to exit")
}

func main() {
	dbLog := waLog.Stdout("Database", "DEBUG", true)
	ctx := context.Background()
	container, err := sqlstore.New(ctx, "sqlite3", "file:store.db?_foreign_keys=on", dbLog)
	if err != nil {
		panic(err)
	}

	deviceStore, err := container.GetFirstDevice(ctx)
	if err != nil {
		panic(err)
	}

	clientLog := waLog.Stdout("Client", "DEBUG", true)
	client = whatsmeow.NewClient(deviceStore, clientLog)
	client.AddEventHandler(eventHandler)

	if client.Store.ID == nil {
		fmt.Println("New device - scanning QR code...")
		qrChan, _ := client.GetQRChannel(context.Background())
		err := client.Connect()
		if err != nil {
			panic(err)
		}
		for evt := range qrChan {
			if evt.Event == "code" {
				displayQRCode(evt.Code)
			} else {
				fmt.Printf("Login event: %s\n", evt.Event)
			}
		}
	} else {
		fmt.Println("Already logged in, connecting...")
		err = client.Connect()
		if err != nil {
			panic(err)
		}
		fmt.Println("Connected!")
	}

	if len(os.Args) > 2 {
		command := os.Args[1]
		if command == "send" && len(os.Args) >= 4 {
			phoneNumber := os.Args[2]
			message := os.Args[3]
			sendMessage(phoneNumber, message)
		} else if command == "send-file" && len(os.Args) >= 4 {
			phoneNumber := os.Args[2]
			filePath := os.Args[3]
			caption := ""
			if len(os.Args) > 4 {
				caption = os.Args[4]
			}
			sendFile(phoneNumber, filePath, caption)
		} else if command == "send-image" && len(os.Args) >= 4 {
			phoneNumber := os.Args[2]
			filePath := os.Args[3]
			caption := ""
			if len(os.Args) > 4 {
				caption = os.Args[4]
			}
			sendImage(phoneNumber, filePath, caption)
		} else {
			printUsage()
		}
		return
	}

	printUsage()
}

func printUsage() {
	fmt.Println("\nUsage:")
	fmt.Println("  ./wsend                    - Login/scan QR code")
	fmt.Println("  ./wsend send <phone> <msg>   - Send text message")
	fmt.Println("  ./wsend send-image <phone> <file> [caption] - Send image")
	fmt.Println("  ./wsend send-file <phone> <file> [caption]  - Send file/document")
	fmt.Println("\nExamples:")
	fmt.Println("  ./wsend send 1234567890 \"Hello!\"")
	fmt.Println("  ./wsend send-image 1234567890 photo.jpg \"Check this out\"")
	fmt.Println("  ./wsend send-file 1234567890 document.pdf")
	fmt.Println("\nWaiting for messages... Press Ctrl+C to exit")

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	client.Disconnect()
	fmt.Println("Disconnected")
}

func sendMessage(phoneNumber, message string) {
	recipient := types.NewJID(phoneNumber, "s.whatsapp.net")

	_, err := client.SendMessage(context.Background(), recipient, &waE2E.Message{
		Conversation: &message,
	})
	if err != nil {
		fmt.Printf("Error sending message: %v\n", err)
	} else {
		fmt.Printf("Message sent to %s: %s\n", recipient, message)
	}

	client.Disconnect()
}

func sendImage(phoneNumber, filePath, caption string) {
	recipient := types.NewJID(phoneNumber, "s.whatsapp.net")

	data, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		return
	}

	uploaded, err := client.UploadMedia(context.Background(), data, whatsmeow.ImageMedia)
	if err != nil {
		fmt.Printf("Error uploading media: %v\n", err)
		return
	}

	msg := &waE2E.Message{
		ImageMessage: &waE2E.ImageMessage{
			URL:         &uploaded.URL,
			DirectPath:  &uploaded.DirectPath,
			MediaKey:    uploaded.MediaKey,
			MimeType:    &uploaded.MimeType,
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:   uploaded.FileSHA256,
			FileName:    func() *string { s := filepath.Base(filePath); return &s }(),
			Caption:     func() *string { if caption != "" { return &caption }; return nil }(),
		},
	}

	_, err = client.SendMessage(context.Background(), recipient, msg)
	if err != nil {
		fmt.Printf("Error sending image: %v\n", err)
	} else {
		fmt.Printf("Image sent to %s: %s\n", recipient, filePath)
	}

	client.Disconnect()
}

func sendFile(phoneNumber, filePath, caption string) {
	recipient := types.NewJID(phoneNumber, "s.whatsapp.net")

	data, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		return
	}

	uploaded, err := client.UploadMedia(context.Background(), data, whatsmeow.DocumentMedia)
	if err != nil {
		fmt.Printf("Error uploading media: %v\n", err)
		return
	}

	msg := &waE2E.Message{
		DocumentMessage: &waE2E.DocumentMessage{
			URL:           &uploaded.URL,
			DirectPath:    &uploaded.DirectPath,
			MediaKey:      uploaded.MediaKey,
			MimeType:      &uploaded.MimeType,
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:   uploaded.FileSHA256,
			FileName:     func() *string { s := filepath.Base(filePath); return &s }(),
			Caption:      func() *string { if caption != "" { return &caption }; return nil }(),
		},
	}

	_, err = client.SendMessage(context.Background(), recipient, msg)
	if err != nil {
		fmt.Printf("Error sending file: %v\n", err)
	} else {
		fmt.Printf("File sent to %s: %s\n", recipient, filePath)
	}

	client.Disconnect()
}