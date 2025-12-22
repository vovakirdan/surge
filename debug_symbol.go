package main

import (
    "context"
    "fmt"
    "os"
    "regexp"
    "strconv"

    "surge/internal/driver"
    "surge/internal/mono"
    "surge/internal/source"
    "surge/internal/symbols"
)

func main() {
    file := "testdata/golden/vm_strings/strings_std.sg"
    opts := driver.DiagnoseOptions{
        Stage:              driver.DiagnoseStageSema,
        EmitHIR:            true,
        EmitInstantiations: true,
    }
    res, err := driver.DiagnoseWithOptions(context.Background(), file, opts)
    if err != nil {
        fmt.Printf("diagnose error: %v\n", err)
        os.Exit(1)
    }
    if res.Bag != nil && res.Bag.HasErrors() {
        fmt.Println("diagnose had errors")
        for _, d := range res.Bag.Items() {
            fmt.Printf("%s\n", d.Message)
        }
        os.Exit(1)
    }
    hirMod, err := driver.CombineHIRWithCore(context.Background(), res)
    if err != nil {
        fmt.Printf("combine error: %v\n", err)
        os.Exit(1)
    }
    if hirMod == nil {
        hirMod = res.HIR
    }
    _, err = mono.MonomorphizeModule(hirMod, res.Instantiations, res.Sema, mono.Options{MaxDepth: 64})
    if err != nil {
        fmt.Printf("mono error: %v\n", err)
        re := regexp.MustCompile(`symbol ([0-9]+)`) 
        if m := re.FindStringSubmatch(err.Error()); len(m) == 2 {
            if id64, parseErr := strconv.ParseUint(m[1], 10, 32); parseErr == nil {
                dumpSymbol(res, symbols.SymbolID(id64))
            }
        }
    }

    dumpSymbol(res, symbols.SymbolID(1231))
}

func dumpSymbol(res *driver.DiagnoseResult, id symbols.SymbolID) {
    if res == nil || res.Symbols == nil || res.Symbols.Table == nil || res.Symbols.Table.Symbols == nil {
        fmt.Println("no symbols table")
        return
    }
    sym := res.Symbols.Table.Symbols.Get(id)
    if sym == nil {
        fmt.Printf("symbol %d not found\n", id)
        return
    }
    name := ""
    if sym.Name != source.NoStringID && res.Symbols.Table.Strings != nil {
        if n, ok := res.Symbols.Table.Strings.Lookup(sym.Name); ok {
            name = n
        }
    }
    fmt.Printf("symbol %d name=%q kind=%d recv=%q typeParams=%d module=%q flags=%v\n", id, name, sym.Kind, sym.ReceiverKey, len(sym.TypeParams), sym.ModulePath, sym.Flags)
    if sym.Signature != nil {
        fmt.Printf("signature: params=%v result=%v hasSelf=%v\n", sym.Signature.Params, sym.Signature.Result, sym.Signature.HasSelf)
    }
}
