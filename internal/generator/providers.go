package generator

import (
	"math/rand"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/brianvoe/gofakeit/v7"
)

// ---------------------------------------------------------------------------
// person.name
// ---------------------------------------------------------------------------

type personNameGen struct{}

func newPersonNameGen(locale string) *personNameGen { return &personNameGen{} }

func (g *personNameGen) Generate(_ Params, r *rand.Rand) any {
	return gofakeit.New(uint64(r.Int63())).Name()
}

// ---------------------------------------------------------------------------
// internet.email
// ---------------------------------------------------------------------------

type emailGen struct{}

func newEmailGen(locale string) *emailGen { return &emailGen{} }

func (g *emailGen) Generate(p Params, r *rand.Rand) any {
	f := gofakeit.New(uint64(r.Int63()))
	domain := p.String("domain", "")
	seq := strconv.FormatInt(r.Int63()&0x7FFFFFFFFFFFFFFF, 36)

	if domain != "" {
		return f.Username() + "+" + seq + "@" + domain
	}

	email := f.Email()
	at := strings.LastIndex(email, "@")
	if at >= 0 {
		return email[:at] + "+" + seq + email[at:]
	}
	return email
}

// ---------------------------------------------------------------------------
// phone.mobile
// ---------------------------------------------------------------------------

type phoneGen struct{}

func newPhoneGen() *phoneGen { return &phoneGen{} }

func (g *phoneGen) Generate(p Params, r *rand.Rand) any {
	format := p.String("format", "##########")
	var out []byte
	for i := 0; i < len(format); i++ {
		switch format[i] {
		case '#':
			out = append(out, byte('0'+r.Intn(10)))
		default:
			out = append(out, format[i])
		}
	}
	return string(out)
}

// ---------------------------------------------------------------------------
// time.date
// ---------------------------------------------------------------------------

type dateGen struct{}

func (g *dateGen) Generate(p Params, r *rand.Rand) any {
	minAge := p.Int("min_age", 18)
	maxAge := p.Int("max_age", 90)
	if minAge > maxAge {
		maxAge = minAge
	}

	now := time.Now()
	minDate := now.AddDate(-maxAge, 0, 0)
	maxDate := now.AddDate(-minAge, 0, 0)
	days := int(maxDate.Sub(minDate).Hours() / 24)

	offset := 0
	if days > 0 {
		offset = r.Intn(days)
	}
	return minDate.AddDate(0, 0, offset).Format("2006-01-02")
}

// ---------------------------------------------------------------------------
// time.timestamp
// ---------------------------------------------------------------------------

type timestampGen struct{}

const tsLayout = "2006-01-02 15:04:05"

func (g *timestampGen) Generate(p Params, r *rand.Rand) any {
	minStr := p.String("min", "")
	maxStr := p.String("max", "now")

	now := time.Now().Truncate(time.Second)

	var minTime, maxTime time.Time
	if minStr == "" {
		minTime = now.AddDate(-1, 0, 0)
	} else {
		var err error
		minTime, err = time.Parse(tsLayout, minStr)
		if err != nil {
			minTime = now.AddDate(-1, 0, 0)
		}
	}

	if maxStr == "now" {
		maxTime = now
	} else {
		var err error
		maxTime, err = time.Parse(tsLayout, maxStr)
		if err != nil {
			maxTime = now
		}
	}

	if !minTime.Before(maxTime) {
		return minTime.Format(tsLayout)
	}

	delta := maxTime.Sub(minTime)
	return minTime.Add(time.Duration(r.Int63n(int64(delta)))).Truncate(time.Second).Format(tsLayout)
}

// ---------------------------------------------------------------------------
// finance.amount
// ---------------------------------------------------------------------------

type amountGen struct{}

func (g *amountGen) Generate(p Params, r *rand.Rand) any {
	minVal := p.Float("min", 0.0)
	maxVal := p.Float("max", 1000000.0)
	decimals := p.Int("decimals", 2)

	if minVal > maxVal {
		maxVal = minVal
	}

	val := minVal + r.Float64()*(maxVal-minVal)
	pow := 1.0
	for i := 0; i < decimals; i++ {
		pow *= 10
	}
	return float64(int(val*pow+0.5)) / pow
}

// ---------------------------------------------------------------------------
// finance.credit_card
// ---------------------------------------------------------------------------

type creditCardGen struct{}

func newCreditCardGen(locale string) *creditCardGen { return &creditCardGen{} }

func (g *creditCardGen) Generate(_ Params, r *rand.Rand) any {
	return gofakeit.New(uint64(r.Int63())).CreditCardNumber(nil)
}

// ---------------------------------------------------------------------------
// uuid
// ---------------------------------------------------------------------------

type uuidGen struct{}

func (g *uuidGen) Generate(_ Params, r *rand.Rand) any {
	return gofakeit.New(uint64(r.Int63())).UUID()
}

// ---------------------------------------------------------------------------
// autoincrement
// ---------------------------------------------------------------------------

type autoIncrementGen struct {
	counter atomic.Int64
	init    atomic.Bool
}

func (g *autoIncrementGen) Generate(p Params, r *rand.Rand) any {
	start := p.Int("start", 1)
	step := p.Int("step", 1)

	if !g.init.Load() {
		if g.init.CompareAndSwap(false, true) {
			g.counter.Store(int64(start - step))
		}
	}
	return g.counter.Add(int64(step))
}

// ---------------------------------------------------------------------------
// collection.random_choice
// ---------------------------------------------------------------------------

type randomChoiceGen struct{}

func (g *randomChoiceGen) Generate(p Params, r *rand.Rand) any {
	choices := p.StringSlice("choices")
	if len(choices) == 0 {
		return nil
	}
	weights := p.FloatSlice("weights")

	if len(weights) == len(choices) && len(weights) > 0 {
		total := 0.0
		for _, w := range weights {
			total += w
		}
		roll := r.Float64() * total
		cum := 0.0
		for i, w := range weights {
			cum += w
			if roll <= cum {
				return choices[i]
			}
		}
	}
	return choices[r.Intn(len(choices))]
}

// ---------------------------------------------------------------------------
// static.value
// ---------------------------------------------------------------------------

type staticValueGen struct{}

func (g *staticValueGen) Generate(p Params, _ *rand.Rand) any {
	return p["value"]
}

// ---------------------------------------------------------------------------
// relation.foreign_key
// ---------------------------------------------------------------------------

type PoolProvider func(tableName string) IDPool

type foreignKeyGen struct{}

func (g *foreignKeyGen) Generate(p Params, r *rand.Rand) any {
	table := p.String("table", "")
	provider, ok := p["__pool_provider"].(PoolProvider)
	if !ok || provider == nil {
		return nil
	}
	pool := provider(table)
	if pool == nil {
		return nil
	}
	return pool.Sample(r)
}
