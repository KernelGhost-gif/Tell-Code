#include "codegen.hpp"

#include <llvm/ADT/StringRef.h>
#include <llvm/Support/ConvertUTF.h>
#include <llvm/Support/JSON.h>
#include <llvm/Support/raw_ostream.h>

#include <algorithm>
#include <cstdint>
#include <iostream>
#include <map>
#include <optional>
#include <string>
#include <utility>

namespace {
using llvm::StringRef;
namespace json = llvm::json;

constexpr StringRef keywords[] = {
    "let", "let number", "let decimal", "let text", "let bool", "object",
    "own", "others", "end object", "to", "out", "end function", "self", "if",
    "otherwise", "end if", "while", "end while", "return", "give back",
    "say", "true", "false", "extern cpp function", "extern c function",
};

struct DiagnosticRange {
    size_t line = 0;
    size_t start = 0;
    size_t end = 0;
};

DiagnosticRange diagnostic_range(StringRef source, tell::Location location) {
    DiagnosticRange range;
    auto parts = source.split('\n');
    while (range.line + 1 < location.line && parts.first.size() < source.size()) {
        source = parts.second;
        ++range.line;
        parts = source.split('\n');
    }

    StringRef line = parts.first.rtrim('\r');
    const size_t target = std::min(location.column - 1, line.size());
    size_t offset = 0;
    while (offset < line.size()) {
        const unsigned bytes = llvm::getNumBytesForUTF8(line[offset]);
        if (offset + bytes > target) {
            range.end = range.start + (bytes == 4 ? 2 : 1);
            return range;
        }
        offset += bytes;
        range.start += bytes == 4 ? 2 : 1;
    }
    range.end = range.start;
    return range;
}

json::Object diagnostic(StringRef message, DiagnosticRange range = {}) {
    json::Object positions{
        {"start", json::Object{{"line", range.line}, {"character", range.start}}},
        {"end", json::Object{{"line", range.line}, {"character", range.end}}},
    };
    return json::Object{{"range", std::move(positions)},
            {"severity", 1}, {"source", "tellc"},
            {"message", json::isUTF8(message) ? message.str() : json::fixUTF8(message)}};
}

json::Array diagnostics(StringRef source) {
    try {
        tell::check_program(tell::parse(source.str()));
        return {};
    } catch (const tell::Error& error) {
        return json::Array{diagnostic(error.what(), diagnostic_range(source, error.location))};
    } catch (const std::exception& error) {
        return json::Array{diagnostic(std::string("tellc: ") + error.what())};
    }
}

class Server {
    std::map<std::string, std::string> documents;
    bool shutdown_requested = false;

    static void send(json::Object message) {
        std::string body;
        llvm::raw_string_ostream(body) << json::Value(std::move(message));
        std::cout << "Content-Length: " << body.size() << "\r\n\r\n" << body << std::flush;
    }

    static void publish(StringRef uri, json::Array items,
                        std::optional<int64_t> version = std::nullopt) {
        json::Object params{{"uri", uri}, {"diagnostics", std::move(items)}};
        if (version) params["version"] = *version;
        send(json::Object{{"jsonrpc", "2.0"}, {"method", "textDocument/publishDiagnostics"},
              {"params", std::move(params)}});
    }

    void update_document(StringRef method, const json::Object* params) {
        const auto* document = params ? params->getObject("textDocument") : nullptr;
        if (!document) return;
        auto uri = document->getString("uri");
        if (!uri) return;

        if (method == "textDocument/didClose") {
            documents.erase(uri->str());
            publish(*uri, {});
            return;
        }

        std::optional<StringRef> source;
        if (method == "textDocument/didOpen") {
            source = document->getString("text");
        } else {
            if (documents.find(uri->str()) == documents.end()) return;
            const auto* changes = params->getArray("contentChanges");
            if (!changes || changes->empty()) return;
            const auto* change = changes->back().getAsObject();
            if (change && !change->get("range")) source = change->getString("text");
        }
        if (!source) return;

        auto& buffer = documents[uri->str()];
        buffer = source->str();
        publish(*uri, diagnostics(buffer), document->getInteger("version"));
    }

    bool handle(const json::Object& message) {
        auto method = message.getString("method");
        if (!method) return true;
        if (*method == "exit") return false;

        const auto* id = message.get("id");
        json::Value result = nullptr;
        if (*method == "initialize") {
            result = json::Object{
                {"capabilities", json::Object{
                                     {"textDocumentSync", json::Object{{"openClose", true}, {"change", 1}}},
                                     {"completionProvider", json::Object{}},
                                 }},
                {"serverInfo", json::Object{{"name", "tell-lsp"}, {"version", "0.1.0"}}},
            };
        } else if (*method == "shutdown") {
            shutdown_requested = true;
        } else if (*method == "textDocument/didOpen" || *method == "textDocument/didChange" ||
                   *method == "textDocument/didClose") {
            update_document(*method, message.getObject("params"));
        } else if (*method == "textDocument/completion") {
            json::Array items;
            for (StringRef word : keywords) items.push_back(json::Object{{"label", word}, {"kind", 14}});
            result = std::move(items);
        } else if (id) {
            send(json::Object{{"jsonrpc", "2.0"}, {"id", *id},
                  {"error", json::Object{{"code", -32601}, {"message", "Method not found"}}}});
            return true;
        }
        if (id) send(json::Object{{"jsonrpc", "2.0"}, {"id", *id}, {"result", std::move(result)}});
        return true;
    }

public:
    int run() {
        // Allow large editor buffers while bounding the allocation from a frame header.
        constexpr size_t max_message_size = 32 * 1024 * 1024;
        for (;;) {
            std::optional<size_t> length;
            for (std::string header;;) {
                if (!std::getline(std::cin, header))
                    return shutdown_requested && header.empty() && !length ? 0 : 1;
                auto line = StringRef(header).rtrim('\r');
                if (line.empty()) break;
                auto separator = line.find(':');
                if (separator == StringRef::npos) return 1;
                auto fields = line.split(':');
                if (fields.first.trim().equals_insensitive("Content-Length")) {
                    size_t size;
                    if (length || fields.second.trim().getAsInteger(10, size) || size > max_message_size)
                        return 1;
                    length = size;
                }
            }
            if (!length) return 1;
            std::string body(*length, '\0');
            if (!std::cin.read(body.data(), body.size())) return 1;
            auto value = json::parse(body);
            if (!value) {
                llvm::consumeError(value.takeError());
                return 1;
            }
            const auto* message = value->getAsObject();
            if (!message) return 1;
            if (!handle(*message)) return shutdown_requested ? 0 : 1;
        }
    }
};
} // namespace

int main() {
    std::cin.tie(nullptr);
    return Server{}.run();
}
