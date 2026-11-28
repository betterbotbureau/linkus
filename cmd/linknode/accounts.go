package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/betterbotbureau/linkus/internal/identity"
	"github.com/betterbotbureau/linkus/internal/node"
)

// `linknode accounts`: the link.us accounts that see this node (on
// link.us/account). It asks Link.us directly, signed with the node's key, so it
// works whether or not the node is running.

const accountsHelp = `Usage: linknode accounts [command]

  (none) | list     the link.us accounts that see this node
  code              show a code: type it on link.us/account under Add a node
                    (works once, for 15 minutes; a new code cancels the last)
  remove <email>    that account no longer sees this node

An account sees whether the node is running, its version and visitor
reports — never what it serves — and cannot change anything here. An
account that added the node can list it in Explore (link.us/explore).`

func init() {
	addCommand("public", command{"accounts", "accounts [command] link.us accounts that see this node (`linknode accounts help`)", cmdAccounts})
}

func cmdAccounts(args []string) error {
	var words []string
	for len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		words, args = append(words, args[0]), args[1:]
	}
	if len(words) > 0 && (words[0] == "help" || words[0] == "-h") {
		fmt.Println(accountsHelp)
		return nil
	}
	cfg, err := parseConfig("accounts", args, nil)
	if err != nil {
		return err
	}
	id, err := identity.Load(cfg.DataDir)
	if err != nil || id.NodeID == "" {
		return errors.New("this node has no address yet: run `linknode run` until it is online once")
	}
	acc := node.NewAccounts(cfg.Server, id, version, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sub := "list"
	if len(words) > 0 {
		sub, words = words[0], words[1:]
	}
	switch sub {
	case "list":
		if err := acc.Refresh(ctx); err != nil {
			return err
		}
		return printAccounts(acc.State())
	case "code":
		code, exp, err := acc.Code(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("Code: %s\n\nSign in on link.us/account and type it under Add a node.\nIt works once, until %s.\n",
			code, exp.Local().Format("15:04"))
		return nil
	case "remove":
		if len(words) != 1 {
			return errors.New("usage: linknode accounts remove <email>")
		}
		if err := acc.Remove(ctx, words[0]); err != nil {
			return err
		}
		fmt.Printf("%s no longer sees this node.\n\n", words[0])
		return printAccounts(acc.State())
	}
	return fmt.Errorf("unknown command %q; see `linknode accounts help`", sub)
}

func printAccounts(st node.AccountsState) error {
	if st.Error != "" {
		fmt.Printf("Link.us: %s\n", st.Error)
	}
	if !st.Known {
		fmt.Println("Not checked yet (the node needs to be online once).")
		return nil
	}
	if len(st.Accounts) == 0 {
		fmt.Println("No link.us account sees this node. `linknode accounts code` shows a code to add it.")
		return nil
	}
	fmt.Println("Link.us accounts that see this node:")
	for _, a := range st.Accounts {
		listed := ""
		if a.Listed {
			listed = "  · lists it in Explore"
		}
		fmt.Printf("  %-40s added %s%s\n", a.Email, a.Added.Local().Format("2006-01-02"), listed)
	}
	return nil
}
