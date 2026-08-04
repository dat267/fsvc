package cmd

import (
	"context"
	"fmt"

	"fsvc/internal/fsapi"
)

type UsersCmdGroup struct {
	Show UsersShowCmd `cmd:"" help:"Show a user"`
}

type UsersShowCmd struct {
	Format string `help:"Output format" enum:"table,json,csv" default:"table"`
	ID     int64  `arg:"" help:"User ID"`
}

var userColumns = []Column{
	{Header: "ID", Path: "id"},
	{Header: "Name", Path: "name"},
	{Header: "Email", Path: "primary_email"},
	{Header: "Job Title", Path: "job_title"},
	{Header: "Department", Path: "department_id"},
	{Header: "Active", Path: "active"},
}

func (c *UsersShowCmd) Run(ctx context.Context, client *fsapi.Client) error {
	path := fmt.Sprintf("users/%d", c.ID)
	data, err := client.Get(ctx, path, nil)
	if err != nil {
		return err
	}
	return Print(data, "user", userColumns, c.Format)
}
