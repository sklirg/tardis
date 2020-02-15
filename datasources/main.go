package datasources

import (
	"context"
	"fmt"
	"os"
	"sync"

	"google.golang.org/api/option"
	sheets "google.golang.org/api/sheets/v4"
)

// GoogleDriveSheets is a simple wrapper around a Google Drive Sheet
// and its data
type GoogleDriveSheets struct {
	client *sheets.Service
	init   sync.Once
	Data   [][]string
}

// FetchSheetData fetches the rows and cells of a Google Sheet as
// a multidimensional array of strings.
func (sheet *GoogleDriveSheets) FetchSheetData(sheetID, sheetRange string) [][]string {
	sheet.init.Do(func() {
		sheet.client = initializeSheetsAPIClient()
	})

	s := sheet.client.Spreadsheets.Values.Get(sheetID, sheetRange)

	vals, err := s.Do()

	if err != nil {
		fmt.Println("Failed to fetch data", err)
	}

	var data [][]string

	for _, v := range vals.Values {

		row := make([]string, 0)

		for _, c := range v {
			d, ok := c.(string)
			if !ok {
				continue
			}
			row = append(row, d)
		}

		data = append(data, row)
	}

	return data
}

func initializeSheetsAPIClient() *sheets.Service {
	credentialsPath := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	ctx := context.Background()
	client, err := sheets.NewService(ctx, option.WithCredentialsFile(credentialsPath))

	if err != nil {
		fmt.Println("Failed to set up Google Sheets client", err)
	}

	return client
}
