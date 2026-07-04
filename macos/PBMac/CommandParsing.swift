import Foundation

// Splits a command line into argv the way a shell would, honoring single quotes,
// double quotes, and backslash escapes — so `backup --exclude '*.log' a:"/My Data"`
// tokenizes the same in the console as in a terminal.
enum CommandTokenizer {
    static func tokenize(_ line: String) -> [String] {
        var tokens: [String] = []
        var current = ""
        var hasToken = false
        var inSingle = false
        var inDouble = false

        let chars = Array(line)
        var i = 0
        while i < chars.count {
            let c = chars[i]
            if inSingle {
                if c == "'" { inSingle = false } else { current.append(c); hasToken = true }
            } else if inDouble {
                if c == "\"" {
                    inDouble = false
                } else if c == "\\", i + 1 < chars.count, chars[i + 1] == "\"" || chars[i + 1] == "\\" {
                    current.append(chars[i + 1]); i += 1; hasToken = true
                } else {
                    current.append(c); hasToken = true
                }
            } else {
                switch c {
                case "'": inSingle = true; hasToken = true
                case "\"": inDouble = true; hasToken = true
                case " ", "\t", "\n", "\r":
                    if hasToken { tokens.append(current); current = ""; hasToken = false }
                case "\\":
                    if i + 1 < chars.count { current.append(chars[i + 1]); i += 1; hasToken = true }
                default: current.append(c); hasToken = true
                }
            }
            i += 1
        }
        if hasToken { tokens.append(current) }
        return tokens
    }

    /// Tokenizes and drops a leading `pbmac`, so both `pbmac list` and `list` work.
    static func pbmacArgs(_ line: String) -> [String] {
        var args = tokenize(line)
        if args.first?.lowercased() == "pbmac" { args.removeFirst() }
        return args
    }
}
