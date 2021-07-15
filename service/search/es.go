package search

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/gofrs/uuid"
	"github.com/olivere/elastic/v7"
	"github.com/traPtitech/traQ/repository"
	"github.com/traPtitech/traQ/service/channel"
	"github.com/traPtitech/traQ/service/message"
	"go.uber.org/zap"
)

const (
	esRequiredVersion = "7.10.2"
	esIndexPrefix     = "traq_"
	esMessageIndex    = "message"
	esDateFormat      = "2006-01-02T15:04:05Z"
)

func getIndexName(index string) string {
	return esIndexPrefix + index
}

// ESEngineConfig Elasticsearch検索エンジン設定
type ESEngineConfig struct {
	// URL ESのURL
	URL string
}

// esEngine search.Engine 実装
type esEngine struct {
	client *elastic.Client
	mm     message.Manager
	cm     channel.Manager
	repo   repository.Repository
	l      *zap.Logger
	done   chan<- struct{}
}

// esMessageDoc Elasticsearchに入るメッセージの情報
type esMessageDoc struct {
	ID             uuid.UUID   `json:"-"`
	UserID         uuid.UUID   `json:"userId"`
	ChannelID      uuid.UUID   `json:"channelId"`
	IsPublic       bool        `json:"isPublic"`
	Bot            bool        `json:"bot"`
	Text           string      `json:"text"`
	CreatedAt      time.Time   `json:"createdAt"`
	UpdatedAt      time.Time   `json:"updatedAt"`
	To             []uuid.UUID `json:"to"`
	Citation       []uuid.UUID `json:"citation"`
	HasURL         bool        `json:"hasURL"`
	HasAttachments bool        `json:"hasAttachments"`
	HasImage       bool        `json:"hasImage"`
	HasVideo       bool        `json:"hasVideo"`
	HasAudio       bool        `json:"hasAudio"`
}

// esMessageDocUpdate Update用 Elasticsearchに入るメッセージの部分的な情報
type esMessageDocUpdate struct {
	Text           string      `json:"text"`
	UpdatedAt      time.Time   `json:"updatedAt"`
	Citation       []uuid.UUID `json:"citation"`
	HasURL         bool        `json:"hasURL"`
	HasAttachments bool        `json:"hasAttachments"`
	HasImage       bool        `json:"hasImage"`
	HasVideo       bool        `json:"hasVideo"`
	HasAudio       bool        `json:"hasAudio"`
}

type m map[string]interface{}

// esMapping Elasticsearchに入るメッセージの情報
// esMessageDoc と同じにする
var esMapping = m{
	"properties": m{
		"userId": m{
			"type": "keyword",
		},
		"channelId": m{
			"type": "keyword",
		},
		"isPublic": m{
			"type": "boolean",
		},
		"bot": m{
			"type": "boolean",
		},
		"text": m{
			"type":     "text",
			"analyzer": "sudachi_analyzer",
		},
		"createdAt": m{
			"type":   "date",
			"format": "strict_date_optional_time_nanos", // 2006-01-02T15:04:05.7891011Z
		},
		"updatedAt": m{
			"type":   "date",
			"format": "strict_date_optional_time_nanos",
		},
		"to": m{
			"type": "keyword",
		},
		"citation": m{
			"type": "keyword",
		},
		"hasURL": m{
			"type": "boolean",
		},
		"hasAttachments": m{
			"type": "boolean",
		},
		"hasImage": m{
			"type": "boolean",
		},
		"hasVideo": m{
			"type": "boolean",
		},
		"hasAudio": m{
			"type": "boolean",
		},
	},
}

// esSetting Indexに追加するsetting情報
var esSetting = m{
	"index": m{
		"analysis": m{
			"tokenizer": m{
				"sudachi_tokenizer": m{
					"type": "sudachi_tokenizer",
				},
			},
			"filter": m{
				"sudachi_split_filter": m{
					"type": "sudachi_split",
					"mode": "search",
				},
			},
			"analyzer": m{
				"sudachi_analyzer": m{
					"tokenizer": "sudachi_tokenizer",
					"type":      "custom",
					"filter": []string{
						"sudachi_split_filter",
						"sudachi_normalizedform",
					},
					"discard_punctuation": true,
					"resources_path":      "/usr/share/elasticsearch/plugins/analysis-sudachi/",
					"settings_path":       "/usr/share/elasticsearch/plugins/analysis-sudachi/sudachi.json",
				},
			},
		},
	},
}

// NewESEngine Elasticsearch検索エンジンを生成します
func NewESEngine(mm message.Manager, cm channel.Manager, repo repository.Repository, logger *zap.Logger, config ESEngineConfig) (Engine, error) {
	// es接続
	client, err := elastic.NewClient(elastic.SetURL(config.URL))
	if err != nil {
		return nil, fmt.Errorf("failed to init search engine: %w", err)
	}

	// esバージョン確認
	version, err := client.ElasticsearchVersion(config.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch es version: %w", err)
	}
	if esRequiredVersion != version {
		return nil, fmt.Errorf("failed to init search engine: version mismatch (%s)", version)
	}

	// index確認
	if exists, err := client.IndexExists(getIndexName(esMessageIndex)).Do(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to init search engine: %w", err)
	} else if !exists {
		// index作成
		r1, err := client.CreateIndex(getIndexName(esMessageIndex)).BodyJson(m{
			"mappings": esMapping,
			"settings": esSetting,
		}).Do(context.Background())
		if err != nil {
			return nil, fmt.Errorf("failed to init search engine: %w", err)
		}
		if !r1.Acknowledged {
			return nil, fmt.Errorf("failed to init search engine: index not acknowledged")
		}
	}

	done := make(chan struct{})
	engine := &esEngine{
		client: client,
		mm:     mm,
		cm:     cm,
		repo:   repo,
		l:      logger.Named("search"),
		done:   done,
	}

	go engine.syncLoop(done)

	return engine, nil
}

func (e *esEngine) Do(q *Query) (Result, error) {
	e.l.Debug("do search", zap.Reflect("q", q))

	var musts []elastic.Query

	if q.Word.Valid {
		musts = append(musts, elastic.NewSimpleQueryStringQuery(q.Word.String).
			Field("text").
			DefaultOperator("AND"))
	}

	switch {
	case q.After.Valid && q.Before.Valid:
		musts = append(musts, elastic.NewRangeQuery("createdAt").
			Gt(q.After.ValueOrZero().Format(esDateFormat)).
			Lt(q.Before.ValueOrZero().Format(esDateFormat)))
	case q.After.Valid && !q.Before.Valid:
		musts = append(musts, elastic.NewRangeQuery("createdAt").
			Gt(q.After.ValueOrZero().Format(esDateFormat)))
	case !q.After.Valid && q.Before.Valid:
		musts = append(musts, elastic.NewRangeQuery("createdAt").
			Lt(q.Before.ValueOrZero().Format(esDateFormat)))
	}

	// チャンネル指定があるときはそのチャンネルを検索
	// そうでないときはPublicチャンネルを検索
	if q.In.Valid {
		musts = append(musts, elastic.NewTermQuery("channelId", q.In))
	} else {
		musts = append(musts, elastic.NewTermQuery("isPublic", true))
	}

	if q.To.Valid {
		musts = append(musts, elastic.NewTermQuery("to", q.To))
	}

	if q.From.Valid {
		musts = append(musts, elastic.NewTermQuery("userId", q.From))
	}

	if q.Citation.Valid {
		musts = append(musts, elastic.NewTermQuery("citation", q.Citation))
	}

	if q.Bot.Valid {
		musts = append(musts, elastic.NewTermQuery("bot", q.Bot))
	}

	if q.HasURL.Valid {
		musts = append(musts, elastic.NewTermQuery("hasURL", q.HasURL))
	}

	if q.HasAttachments.Valid {
		musts = append(musts, elastic.NewTermQuery("hasAttachments", q.HasAttachments))
	}

	if q.HasImage.Valid {
		musts = append(musts, elastic.NewTermQuery("hasImage", q.HasImage))
	}
	if q.HasVideo.Valid {
		musts = append(musts, elastic.NewTermQuery("hasVideo", q.HasVideo))
	}
	if q.HasAudio.Valid {
		musts = append(musts, elastic.NewTermQuery("hasAudio", q.HasAudio))
	}

	limit, offset := 20, 0
	if q.Limit.Valid {
		limit = int(q.Limit.Int64)
	}
	if q.Offset.Valid {
		offset = int(q.Offset.Int64)
	}

	// NOTE: 現状`sort.Key`はそのままesのソートキーとして使える前提
	sort := q.GetSortKey()

	sr, err := e.client.Search().
		Index(getIndexName(esMessageIndex)).
		Query(elastic.NewBoolQuery().Must(musts...)).
		Sort(sort.Key, !sort.Desc).
		Size(limit).
		From(offset).
		Do(context.Background())
	if err != nil {
		return nil, err
	}

	e.l.Debug("search result", zap.Reflect("hits", sr.Hits))
	return e.bindESResult(sr)
}

func (e *esEngine) Available() bool {
	return e.client.IsRunning()
}

func (e *esEngine) Close() error {
	e.client.Stop()
	e.done <- struct{}{}
	return nil
}

type TokenKind int

const (
	err TokenKind = iota
	identifier
	number
	symbol
)

type NodeKind int

const (
	Term NodeKind = iota
	Not
	And
	Or
)

type Node struct {
	kind  NodeKind
	side  [2]*Node
	field string
	value string
}

func parse(source string) *elastic.BoolQuery {
	node, _, err := parseExpression(source)
	if err != nil {
		fmt.Println(err)
		return nil
	}

	query, err := generate(node)
	if err != nil {
		fmt.Println(err)
	}
	e := elastic.NewBoolQuery()
	return e.Must(*query)
}

// <expr> := <clause> <clause>|<expr>
func parseExpression(source string) (*Node, string, error) {
	lhs, peeked, err := parseClause(source)
	if err != nil {
		return lhs, peeked, err
	}

	term, _, peeked := peek(source)
	if term == "|" {
		rhs, peeked, err := parseExpression(peeked)
		return &Node{Or, [2]*Node{lhs, rhs}, "", ""}, peeked, err
	}

	return nil, source, nil
}

// <clause> := <literal> <literal>&<clause>
func parseClause(source string) (*Node, string, error) {
	lhs, peeked, err := parseLiteral(source)
	if err != nil {
		return lhs, peeked, err
	}

	term, _, peeked := peek(source)
	if term == "&" {
		rhs, peeked, err := parseClause(peeked)
		return &Node{And, [2]*Node{lhs, rhs}, "", ""}, peeked, err
	}

	return nil, source, nil
}

// <field> := string
// <value> := string
// <literal> := <field>=<value> -<literal> (<expr>)
func parseLiteral(source string) (*Node, string, error) {
	term, kind, peeked := peek(source)

	if kind != identifier {
		field := term

		term, kind, peeked = peek(peeked)
		if term != "=" {
			return nil, source, fmt.Errorf("Error: expected =, but find %s", term)
		}

		term, kind, peeked = peek(peeked)
		if kind == symbol {
			return nil, source, fmt.Errorf("Error: expected value, but find %s", term)
		}

		value := term
		return &Node{Term, [2]*Node{nil, nil}, field, value}, peeked, nil
	}

	if term == "-" {
		node, peeked, err := parseLiteral(peeked)
		if err != nil {
			return nil, source, err
		}
		return &Node{Not, [2]*Node{node, nil}, "", ""}, peeked, nil
	}

	if term == "(" {
		node, peeked, _ := parseExpression(peeked)
		term, kind, peeked = peek(peeked)
		if term != ")" {
			return nil, peeked, fmt.Errorf("Error: unknown field %s")
		}
		return node, peeked, nil
	}

	return nil, source, nil
}

func generate(node *Node) (*elastic.Query, error) {
	switch node.kind {
	case Term:
		term, err := generateTerm(node.field, node.value)
		if err != nil {
			return nil, err
		}
		return term, nil

	case Not:
		query, err := generate(node.side[0])
		if err != nil {
			return nil, err
		}
		return &elastic.NewBoolQuery().MustNot(*query).Query, nil

	case And:
		lhs, err := generate(node.side[0])
		if err != nil {
			return nil, err
		}
		rhs, err := generate(node.side[1])
		if err != nil {
			return nil, err
		}
		return &elastic.NewBoolQuery().Must(*lhs, *rhs).Query, nil

	case Or:
		lhs, err := generate(node.side[0])
		if err != nil {
			return nil, err
		}
		rhs, err := generate(node.side[1])
		if err != nil {
			return nil, err
		}
		return &elastic.NewBoolQuery().Should(*lhs, *rhs).Query, nil

	default:
		return nil, nil
	}
}

func generateTerm(field string, value string) (*elastic.TermQuery, error) {
	switch field {
	case "word":
		return elastic.NewTermQuery(field, value), nil

	case "after", "before":
		t, err := time.Parse("2006-01-02 15:04:05", value)
		if err != nil {
			return nil, err
		}
		return elastic.NewTermQuery(field, t), nil

	case "in", "to", "from", "citation":
		uuid, err := uuid.FromString(value)
		if err != nil {
			return nil, fmt.Errorf("Error: expected uuid, but find %s", value)
		}
		return elastic.NewTermQuery(field, uuid), nil

	case "bot", "hasurl", "hasattachments", "hasimage", "hasvideo", "hasaudio":
		if value == "true" {
			return elastic.NewTermQuery(field, true), nil
		} else if value == "false" {
			return elastic.NewTermQuery(field, false), nil
		}
		return nil, fmt.Errorf("Error: expected boolean, but find %s", value)

	case "limit", "offset":
		num, err := strconv.Atoi(value)
		if err != nil {
			return nil, err
		}
		return elastic.NewTermQuery(field, num), nil

	case "sort":
		return elastic.NewTermQuery(field, value), nil

	default:
		return nil, fmt.Errorf("Error: unknown field %s", field)
	}
}

func peek(source string) (term string, kind TokenKind, peeked string) {
	checkSymbol, _ := regexp.Compile("[A-Za-z][A-Za-z0-9]*")
	loc := checkSymbol.FindIndex([]byte(source))
	if loc != nil {
		return source[loc[0]:loc[1]], identifier, source[loc[1]:]
	}

	checkNumber, _ := regexp.Compile("[0-9]+")
	loc = checkNumber.FindIndex([]byte(source))
	if loc != nil {
		return source[loc[0]:loc[1]], number, source[loc[1]:]
	}

	switch source[0] {
	case '[', ']', '(', ')', '|', '&', '-', '=':
		return source[0:1], symbol, source[1:]
	case ' ':
		return peek(source[1:])
	default:
		return "", err, source
	}
}
