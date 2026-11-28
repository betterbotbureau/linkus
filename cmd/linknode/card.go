package main

import (
	"fmt"
	"strings"

	"github.com/betterbotbureau/linkus/internal/identity"
	"github.com/betterbotbureau/linkus/internal/node"
)

// `linknode card`: the node's card, the short signed description other
// nodes and agents read (and Explore on link.us shows, if the node is
// listed there). A running node signs it and sends it to Link.us within a
// minute of a change.

const cardHelp = `Usage: linknode card [command]

  (none)                  show this node's card settings
  name <text>             the node's name (up to 80 characters). Without one:
                          "Node <ID>", and once published the site's title
  about <text>            one or two sentences about it (up to 200)

The card is public: anyone can read it at the node's address
(/.well-known/link-card.json), even while the node is off.`

func init() {
	addCommand("public", command{"card", "card [command]   this node's card: its name and description (`linknode card help`)", cmdCard})
}

func cmdCard(args []string) error {
	var words []string
	for len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		words, args = append(words, args[0]), args[1:]
	}
	if len(words) > 0 && (words[0] == "help" || words[0] == "-h") {
		fmt.Println(cardHelp)
		return nil
	}
	cfg, err := parseConfig("card", args, nil)
	if err != nil {
		return err
	}
	id, err := identity.Load(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("no node here yet (run `linknode run` first): %w", err)
	}
	cards := node.OpenNodeCards(cfg.DataDir, id, version, cardNamer(func() string { return cfg.PublicDir }, id), nil)
	if len(words) == 0 {
		return printCard(cards, id)
	}
	sub, rest := words[0], strings.Join(words[1:], " ")
	name, about := cards.CardSettings()
	switch sub {
	case "name":
		err = cards.Set(rest, about)
	case "about":
		err = cards.Set(name, rest)
	default:
		return fmt.Errorf("unknown command %q; see `linknode card help`", sub)
	}
	if err != nil {
		return err
	}
	fmt.Println("Saved. A running node sends the change to Link.us within a minute.")
	fmt.Println()
	return printCard(cards, id)
}

// cardNamer is the card's default name: the site's title, else
// "Node <id>".
func cardNamer(publicDir func() string, id *identity.Identity) func() string {
	return node.OwnerNamer(publicDir, func() string { return "Node " + id.NodeID })
}

func printCard(cards *node.NodeCards, id *identity.Identity) error {
	name, about := cards.CardSettings()
	switch {
	case name != "":
		fmt.Printf("Name:       %s\n", name)
	case node.LoadPublished(cards.DataDir()):
		fmt.Printf("Name:       %s (the default: the site's title, else the node's ID;\n", cards.DefaultName())
		fmt.Println("            `linknode card name …` gives your own)")
	default:
		fmt.Printf("Name:       %s (the default while unpublished; once published, the\n", cards.DefaultName())
		fmt.Printf("            site's title: %q; `linknode card name …` gives your own)\n", cards.PublishedName())
	}
	if about != "" {
		fmt.Printf("About:      %s\n", about)
	}
	if node.ReleaseEdition != "" {
		fmt.Println("Messages:   not taken (the command-line node has no Inbox)")
	}
	if addr := node.SavedAddress(cards.DataDir()); addr != "" && id.NodeID != "" {
		fmt.Printf("Public at:  %s/.well-known/link-card.json\n", strings.TrimSuffix(addr, "/"))
	}
	return nil
}
