"""Tk desktop IDE for editing, running, and interactively exploring Tell."""

from __future__ import annotations

import codecs
import os
import queue
import re
import subprocess
import sys
import tempfile
import threading
import webbrowser
from pathlib import Path

from .ide_runtime import child_environment, command_for, repository_root

try:
    import tkinter as tk
    from tkinter import filedialog, messagebox, simpledialog, ttk
except ImportError as exc:  # Tk is optional for headless Python installations.
    tk = None
    ttk = None
    filedialog = messagebox = simpledialog = None
    _TK_ERROR = str(exc)
else:
    _TK_ERROR = ""

_Frame = ttk.Frame if ttk is not None else object
_Root = tk.Tk if tk is not None else object

_WORDS = {
    "let", "set", "remember", "as", "say", "ask", "save", "the", "answer",
    "if", "otherwise", "while", "repeat", "times", "for", "each", "in", "end",
    "loop", "function", "to", "give", "back", "return", "create", "class", "new",
    "tell", "when", "with", "constructor", "try", "something", "goes", "wrong",
    "finally", "raise", "an", "error", "saying", "stop", "this", "skip", "next",
    "repetition", "number", "use", "module", "called", "and", "or", "not", "is",
    "equal", "greater", "less", "than", "at", "least", "most", "true", "false",
    "nothing", "none", "null", "as", "constant", "const", "static", "method",
}
_BUILTINS = {
    "len", "range", "sum", "min", "max", "sorted", "reversed", "map", "filter",
    "reduce", "abs", "round", "average", "first", "last", "any", "all", "type_of",
    "random_number", "number", "sqrt", "floor", "ceiling", "list", "tuple", "set",
    "dictionary", "str", "int", "float", "bool",
}
_TOKEN = re.compile(
    r'(?P<comment>\#.*$|\bNote:.*$)'
    r'|(?P<string>"(?:\\.|[^"\\])*"|\'(?:\\.|[^\'\\])*\')'
    r'|(?P<number>\b\d+(?:\.\d+)?\b)'
    r'|(?P<word>[^\W\d]\w*|_\w*)',
    re.MULTILINE | re.UNICODE,
)
_BLOCK_START = re.compile(
    r"^(?:if\b|while\b|for\s+each\b|repeat\b|to\b|"
    r"create\s+(?:a\s+)?class\b|try\b|when\s+creating\b).*:\s*$",
    re.IGNORECASE,
)
_BLOCK_END = re.compile(r"^end\b", re.IGNORECASE)


class CodeEditor(_Frame):
    """A Tell-aware text editor with synchronized line numbers and indentation."""

    def __init__(self, master, app, source: str = "", path: Path | None = None):
        super().__init__(master)
        self.app = app
        self.path = path
        self._dirty = False
        self._highlight_job = None
        self.gutter = tk.Text(
            self, width=5, padx=8, takefocus=False, wrap="none", state="disabled",
            bg="#17212d", fg="#74859a", insertbackground="#17212d",
            relief="flat", borderwidth=0, font=("Cascadia Code", 11),
        )
        self.text = tk.Text(
            self, undo=True, autoseparators=True, maxundo=-1, wrap="none",
            bg="#101821", fg="#e6edf3", insertbackground="#f4c95d",
            selectbackground="#315b7d", selectforeground="#ffffff",
            relief="flat", borderwidth=0, padx=12, pady=10,
            font=("Cascadia Code", 11), tabs=(36,),
        )
        self.scrollbar = ttk.Scrollbar(self, orient="vertical", command=self._scroll_both)
        self.horizontal = ttk.Scrollbar(self, orient="horizontal", command=self.text.xview)
        self.text.configure(yscrollcommand=self._sync_scroll, xscrollcommand=self.horizontal.set)
        self.gutter.grid(row=0, column=0, sticky="ns")
        self.text.grid(row=0, column=1, sticky="nsew")
        self.scrollbar.grid(row=0, column=2, sticky="ns")
        self.horizontal.grid(row=1, column=1, sticky="ew")
        self.grid_rowconfigure(0, weight=1)
        self.grid_columnconfigure(1, weight=1)

        self.text.tag_configure("keyword", foreground="#82b6ff")
        self.text.tag_configure("string", foreground="#a8d58c")
        self.text.tag_configure("comment", foreground="#81909f", font=("Cascadia Code", 11, "italic"))
        self.text.tag_configure("number", foreground="#d7a6ff")
        self.text.tag_configure("builtin", foreground="#58d6c0")
        self.text.bind("<Return>", self._return_with_indent)
        self.text.bind("<KeyRelease>", self._on_cursor_or_edit)
        self.text.bind("<ButtonRelease-1>", self._on_cursor_or_edit)
        self.text.bind("<<Modified>>", self._on_modified)
        self.text.insert("1.0", source)
        self.text.edit_modified(False)
        self.refresh_gutter()
        self.schedule_highlight()

    @property
    def modified(self) -> bool:
        return self._dirty

    def content(self) -> str:
        return self.text.get("1.0", "end-1c")

    def set_content(self, source: str) -> None:
        self.text.delete("1.0", "end")
        self.text.insert("1.0", source)
        self.text.edit_modified(False)
        self._dirty = False
        self.refresh_gutter()
        self.schedule_highlight()

    def _sync_scroll(self, first, last):
        self.scrollbar.set(first, last)
        self.gutter.yview_moveto(first)

    def _scroll_both(self, *args):
        self.text.yview(*args)
        self.gutter.yview(*args)

    def refresh_gutter(self):
        final_line = int(self.text.index("end-1c").split(".")[0])
        numbers = "\n".join(str(number) for number in range(1, final_line + 1))
        self.gutter.configure(state="normal")
        self.gutter.delete("1.0", "end")
        self.gutter.insert("1.0", numbers)
        self.gutter.configure(state="disabled")
        self.gutter.yview_moveto(self.text.yview()[0])

    def schedule_highlight(self):
        if self._highlight_job is not None:
            self.after_cancel(self._highlight_job)
        self._highlight_job = self.after(110, self.highlight)

    def highlight(self):
        self._highlight_job = None
        source = self.content()
        for tag in ("keyword", "string", "comment", "number", "builtin"):
            self.text.tag_remove(tag, "1.0", "end")
        for match in _TOKEN.finditer(source):
            token = match.group(0)
            if match.lastgroup == "comment":
                tag = "comment"
            elif match.lastgroup == "string":
                tag = "string"
            elif match.lastgroup == "number":
                tag = "number"
            elif token.casefold() in _BUILTINS:
                tag = "builtin"
            elif token.casefold() in _WORDS:
                tag = "keyword"
            else:
                continue
            start = f"1.0 + {match.start()} chars"
            end = f"1.0 + {match.end()} chars"
            self.text.tag_add(tag, start, end)
        self.refresh_gutter()

    def _on_modified(self, _event=None):
        if self.text.edit_modified():
            self.text.edit_modified(False)
            self._dirty = True
            self.app.document_changed(self)
            self.schedule_highlight()

    def _on_cursor_or_edit(self, _event=None):
        self.refresh_gutter()
        self.app.update_cursor_status(self)
        self.schedule_highlight()

    def _return_with_indent(self, _event=None):
        current = self.text.get("insert linestart", "insert lineend")
        previous_indent = re.match(r"[ \t]*", current).group(0)
        stripped = current.strip()
        indent = len(previous_indent.expandtabs(4))
        if re.match(r"end\b", stripped, re.IGNORECASE):
            indent = max(0, indent - 4)
        elif stripped.endswith(":"):
            indent += 4
        self.text.insert("insert", "\n" + " " * indent)
        return "break"


class ShellPanel(_Frame):
    """Persistent interactive shell with history and stdin routing for running files."""

    def __init__(self, master, app):
        super().__init__(master, padding=(10, 8, 10, 8))
        self.app = app
        self.history: list[str] = []
        self.history_index = 0
        self.block_depth = 0
        self.prompt = tk.StringVar(value=">>>")

        header = ttk.Frame(self)
        header.pack(fill="x", pady=(0, 6))
        ttk.Label(header, text="Tell Shell", style="PanelTitle.TLabel").pack(side="left")
        ttk.Label(header, textvariable=app.backend_label, style="Muted.TLabel").pack(side="left", padx=12)
        ttk.Button(header, text="Restart shell", command=app.restart_shell).pack(side="right")
        ttk.Button(header, text="Clear", command=self.clear).pack(side="right", padx=(0, 6))

        body = ttk.Frame(self)
        body.pack(fill="both", expand=True)
        self.output = tk.Text(
            body, height=8, wrap="word", state="disabled", padx=10, pady=8,
            bg="#0b1118", fg="#d5dee8", insertbackground="#ffffff",
            relief="flat", borderwidth=0, font=("Cascadia Code", 10),
        )
        scroll = ttk.Scrollbar(body, command=self.output.yview)
        self.output.configure(yscrollcommand=scroll.set)
        self.output.pack(side="left", fill="both", expand=True)
        scroll.pack(side="right", fill="y")
        self.output.tag_configure("error", foreground="#ff8c8c")
        self.output.tag_configure("system", foreground="#89a8c5")
        self.output.tag_configure("command", foreground="#f4c95d")

        entry_row = ttk.Frame(self)
        entry_row.pack(fill="x", pady=(7, 0))
        ttk.Label(entry_row, textvariable=self.prompt, width=6, style="Prompt.TLabel").pack(side="left", anchor="n", pady=6)
        self.input = tk.Text(
            entry_row, height=2, wrap="word", undo=True, padx=8, pady=5,
            bg="#111b26", fg="#e6edf3", insertbackground="#f4c95d",
            relief="flat", borderwidth=0, font=("Cascadia Code", 10),
        )
        self.input.pack(side="left", fill="x", expand=True)
        self.input.bind("<Return>", self._enter)
        self.input.bind("<Up>", self._history_up)
        self.input.bind("<Down>", self._history_down)
        self.input.bind("<Control-Return>", self._enter)

    def write(self, text: str, tag: str | None = None):
        if not text:
            return
        self.output.configure(state="normal")
        self.output.insert("end", text, tag or ())
        self.output.see("end")
        self.output.configure(state="disabled")

    def clear(self):
        self.output.configure(state="normal")
        self.output.delete("1.0", "end")
        self.output.configure(state="disabled")
        self.input.focus_set()

    def _enter(self, event=None):
        if event is not None and event.state & 0x0001:
            self.input.insert("insert", "\n")
            return "break"
        source = self.input.get("1.0", "end-1c")
        self.input.delete("1.0", "end")
        if not source.strip():
            return "break"
        if source.strip().casefold() == ":clear" and not self.block_depth:
            self.clear()
            return "break"
        lines = source.splitlines() or [source]
        for line in lines:
            if self.app.run_id in self.app.processes:
                prefix = "input> "
            else:
                prefix = "... " if self.block_depth else ">>> "
            self.write(prefix + line + "\n", "command")
            self.app.send_shell_line(line)
            stripped = line.strip()
            if _BLOCK_END.match(stripped):
                self.block_depth = max(0, self.block_depth - 1)
            elif _BLOCK_START.match(stripped):
                self.block_depth += 1
        if self.history and self.history[-1] == source:
            pass
        else:
            self.history.append(source)
        self.history_index = len(self.history)
        self.prompt.set("..." if self.block_depth else ">>>")
        self.input.focus_set()
        return "break"

    def _history_up(self, _event=None):
        if not self.history:
            return "break"
        self.history_index = max(0, self.history_index - 1)
        self._show_history()
        return "break"

    def _history_down(self, _event=None):
        if not self.history:
            return "break"
        self.history_index = min(len(self.history), self.history_index + 1)
        self._show_history()
        return "break"

    def _show_history(self):
        self.input.delete("1.0", "end")
        if self.history_index < len(self.history):
            self.input.insert("1.0", self.history[self.history_index])
        self.input.mark_set("insert", "end-1c")


class TellIDE(_Root):
    """Main Tell desktop application."""

    def __init__(self):
        super().__init__()
        self.root_dir = repository_root()
        self.project_dir = Path.cwd()
        self.backend_label = tk.StringVar(value="Finding runtime…")
        self.events: queue.Queue = queue.Queue()
        self.processes: dict[int, tuple[subprocess.Popen, str, Path | None]] = {}
        self.repl_id: int | None = None
        self.run_id: int | None = None
        self.next_process_id = 1
        self.last_search = ""
        self._closing = False

        self.title("Tell IDE")
        self.geometry("1280x860")
        self.minsize(900, 620)
        self.configure(bg="#121922")
        self._configure_style()
        self._build_menu()
        self._build_layout()
        self._new_document()
        self.protocol("WM_DELETE_WINDOW", self.close)
        self.bind_all("<Control-n>", lambda _event: self._new_document())
        self.bind_all("<Control-o>", lambda _event: self.open_file())
        self.bind_all("<Control-s>", lambda _event: self.save_current())
        self.bind_all("<Control-Shift-S>", lambda _event: self.save_current(as_new=True))
        self.bind_all("<Control-w>", lambda _event: self.close_current_tab())
        self.bind_all("<Control-f>", lambda _event: self.find_in_editor())
        self.bind_all("<F5>", lambda _event: self.run_current())
        self.bind_all("<F7>", lambda _event: self.check_current())
        self.bind_all("<Control-b>", lambda _event: self.build_current())
        self.bind_all("<Control-t>", lambda _event: self.run_project_tests())
        self.after(80, self._poll_events)
        self.after(200, self.restart_shell)

    def _configure_style(self):
        style = ttk.Style(self)
        try:
            style.theme_use("clam")
        except tk.TclError:
            pass
        style.configure(".", background="#17212d", foreground="#e6edf3", font=("Segoe UI", 10))
        style.configure("TFrame", background="#17212d")
        style.configure("Toolbar.TFrame", background="#202d3b")
        style.configure("TLabel", background="#17212d", foreground="#e6edf3")
        style.configure("Muted.TLabel", background="#17212d", foreground="#95a7ba")
        style.configure("PanelTitle.TLabel", background="#17212d", foreground="#f4c95d", font=("Segoe UI Semibold", 10))
        style.configure("Prompt.TLabel", background="#17212d", foreground="#f4c95d", font=("Cascadia Code", 10, "bold"))
        style.configure("TButton", background="#2a3a4c", foreground="#e6edf3", padding=(10, 6), borderwidth=0)
        style.map("TButton", background=[("active", "#3b5068"), ("pressed", "#477094")])
        style.configure("Accent.TButton", background="#167c80", foreground="#ffffff", font=("Segoe UI Semibold", 10))
        style.map("Accent.TButton", background=[("active", "#1c9699"), ("pressed", "#126669")])
        style.configure("TNotebook", background="#17212d", borderwidth=0)
        style.configure("TNotebook.Tab", background="#202d3b", foreground="#aebdca", padding=(14, 8))
        style.map("TNotebook.Tab", background=[("selected", "#101821")], foreground=[("selected", "#f4c95d")])
        style.configure("TPanedwindow", background="#17212d")

    def _build_menu(self):
        menu = tk.Menu(self, bg="#202d3b", fg="#e6edf3", activebackground="#167c80", activeforeground="#fff", tearoff=False)
        file_menu = tk.Menu(menu, tearoff=False)
        file_menu.add_command(label="New File", accelerator="Ctrl+N", command=self._new_document)
        file_menu.add_command(label="Open…", accelerator="Ctrl+O", command=self.open_file)
        file_menu.add_command(label="Save", accelerator="Ctrl+S", command=self.save_current)
        file_menu.add_command(label="Save As…", accelerator="Ctrl+Shift+S", command=lambda: self.save_current(as_new=True))
        file_menu.add_separator()
        file_menu.add_command(label="Close Tab", accelerator="Ctrl+W", command=self.close_current_tab)
        file_menu.add_command(label="Exit", command=self.close)
        menu.add_cascade(label="File", menu=file_menu)

        edit_menu = tk.Menu(menu, tearoff=False)
        edit_menu.add_command(label="Undo", accelerator="Ctrl+Z", command=lambda: self._text_action("edit_undo"))
        edit_menu.add_command(label="Redo", accelerator="Ctrl+Y", command=lambda: self._text_action("edit_redo"))
        edit_menu.add_separator()
        for label, sequence in (("Cut", "<<Cut>>"), ("Copy", "<<Copy>>"), ("Paste", "<<Paste>>"), ("Select All", "<<SelectAll>>")):
            edit_menu.add_command(label=label, command=lambda seq=sequence: self._text_event(seq))
        edit_menu.add_command(label="Find…", accelerator="Ctrl+F", command=self.find_in_editor)
        menu.add_cascade(label="Edit", menu=edit_menu)

        run_menu = tk.Menu(menu, tearoff=False)
        run_menu.add_command(label="Run Module", accelerator="F5", command=self.run_current)
        run_menu.add_command(label="Check Syntax", accelerator="F7", command=self.check_current)
        run_menu.add_command(label="Build Bytecode", accelerator="Ctrl+B", command=self.build_current)
        run_menu.add_command(label="Run Project Tests", accelerator="Ctrl+T", command=self.run_project_tests)
        run_menu.add_separator()
        run_menu.add_command(label="Stop Running Program", command=self.stop_program)
        run_menu.add_command(label="Restart Shell", command=self.restart_shell)
        menu.add_cascade(label="Run", menu=run_menu)

        help_menu = tk.Menu(menu, tearoff=False)
        help_menu.add_command(label="Tell Language Reference", command=self.open_language_reference)
        help_menu.add_command(label="Tell IDE Guide", command=self.open_ide_guide)
        help_menu.add_separator()
        help_menu.add_command(label="About Tell IDE", command=self.about)
        menu.add_cascade(label="Help", menu=help_menu)
        self.configure(menu=menu)

    def _build_layout(self):
        toolbar = ttk.Frame(self, style="Toolbar.TFrame", padding=(10, 8))
        toolbar.pack(fill="x")
        ttk.Label(toolbar, text="TELL", background="#202d3b", foreground="#f4c95d", font=("Segoe UI Black", 13)).pack(side="left", padx=(2, 14))
        self._tool_button(toolbar, "New", self._new_document)
        self._tool_button(toolbar, "Open", self.open_file)
        self._tool_button(toolbar, "Save", self.save_current)
        ttk.Separator(toolbar, orient="vertical").pack(side="left", fill="y", padx=7)
        self._tool_button(toolbar, "▶ Run  F5", self.run_current, "Accent.TButton")
        self._tool_button(toolbar, "Check  F7", self.check_current)
        self._tool_button(toolbar, "Build", self.build_current)
        self._tool_button(toolbar, "Tests", self.run_project_tests)
        self._tool_button(toolbar, "■ Stop", self.stop_program)
        ttk.Label(toolbar, textvariable=self.backend_label, background="#202d3b", foreground="#95a7ba").pack(side="right", padx=8)

        panes = ttk.Panedwindow(self, orient="vertical")
        panes.pack(fill="both", expand=True, padx=8, pady=(8, 5))
        editor_area = ttk.Frame(panes)
        editor_header = ttk.Frame(editor_area, padding=(10, 7))
        editor_header.pack(fill="x")
        ttk.Label(editor_header, text="Editor", style="PanelTitle.TLabel").pack(side="left")
        self.file_label = ttk.Label(editor_header, text="Untitled Tell source", style="Muted.TLabel")
        self.file_label.pack(side="right")
        self.notebook = ttk.Notebook(editor_area)
        self.notebook.pack(fill="both", expand=True)
        self.notebook.bind("<<NotebookTabChanged>>", self._tab_changed)
        panes.add(editor_area, weight=3)

        self.shell = ShellPanel(panes, self)
        panes.add(self.shell, weight=2)

        status = ttk.Frame(self, padding=(10, 5))
        status.pack(fill="x", side="bottom")
        self.status_left = tk.StringVar(value="Ready")
        self.status_right = tk.StringVar(value="Ln 1, Col 1")
        ttk.Label(status, textvariable=self.status_left, style="Muted.TLabel").pack(side="left")
        ttk.Label(status, textvariable=self.status_right, style="Muted.TLabel").pack(side="right")

    @staticmethod
    def _tool_button(parent, label, command, style="TButton"):
        button = ttk.Button(parent, text=label, command=command, style=style)
        button.pack(side="left", padx=3)
        return button

    def _new_document(self):
        editor = CodeEditor(self.notebook, self)
        self.notebook.add(editor, text="Untitled.tell")
        self.notebook.select(editor)
        editor.text.focus_set()
        self.update_document_labels(editor)
        return editor

    def current_editor(self) -> CodeEditor | None:
        selected = self.notebook.select()
        if not selected:
            return None
        return self.nametowidget(selected)

    def _tab_changed(self, _event=None):
        editor = self.current_editor()
        if editor:
            name = editor.path.name if editor.path else "Untitled Tell source"
            self.file_label.configure(text=name + ("  •" if editor.modified else ""))
            self.update_cursor_status(editor)

    def document_changed(self, editor):
        self.update_document_labels(editor)

    def update_document_labels(self, editor):
        name = editor.path.name if editor.path else "Untitled.tell"
        index = self.notebook.index(editor)
        self.notebook.tab(editor, text=name + (" •" if editor.modified else ""))
        if editor == self.current_editor():
            self.file_label.configure(text=(str(editor.path) if editor.path else "Untitled Tell source") + ("  •" if editor.modified else ""))
        self.title(("*" if editor.modified else "") + f"{name} — Tell IDE")

    def update_cursor_status(self, editor=None):
        editor = editor or self.current_editor()
        if editor:
            line, column = editor.text.index("insert").split(".")
            self.status_right.set(f"Ln {line}, Col {int(column) + 1}")

    def open_file(self):
        filename = filedialog.askopenfilename(
            parent=self, title="Open Tell source", filetypes=[("Tell source", "*.tell"), ("All files", "*.*")]
        )
        if not filename:
            return
        path = Path(filename).resolve()
        for tab_id in self.notebook.tabs():
            existing = self.nametowidget(tab_id)
            if existing.path == path:
                self.notebook.select(existing)
                return
        try:
            source = path.read_text(encoding="utf-8")
        except (OSError, UnicodeError) as exc:
            messagebox.showerror("Open failed", str(exc), parent=self)
            return
        editor = CodeEditor(self.notebook, self, source, path)
        self.notebook.add(editor, text=path.name)
        self.notebook.select(editor)
        self.project_dir = path.parent
        self.status_left.set(f"Opened {path}")
        self.update_document_labels(editor)

    def save_current(self, as_new=False):
        editor = self.current_editor()
        if editor is None:
            return False
        path = editor.path
        if as_new or path is None:
            filename = filedialog.asksaveasfilename(
                parent=self, title="Save Tell source", defaultextension=".tell",
                initialfile=path.name if path else "untitled.tell",
                filetypes=[("Tell source", "*.tell"), ("All files", "*.*")],
            )
            if not filename:
                return False
            path = Path(filename).resolve()
        try:
            path.write_text(editor.content(), encoding="utf-8", newline="\n")
        except (OSError, UnicodeError) as exc:
            messagebox.showerror("Save failed", str(exc), parent=self)
            return False
        editor.path = path
        editor.text.edit_modified(False)
        editor._dirty = False
        self.project_dir = path.parent
        self.update_document_labels(editor)
        self.status_left.set(f"Saved {path}")
        return True

    def _confirm_editor_close(self, editor):
        if not editor.modified:
            return True
        name = editor.path.name if editor.path else "Untitled.tell"
        choice = messagebox.askyesnocancel("Unsaved changes", f"Save changes to {name}?", parent=self)
        if choice is None:
            return False
        if choice:
            self.notebook.select(editor)
            return self.save_current()
        return True

    def close_current_tab(self):
        editor = self.current_editor()
        if editor and self._confirm_editor_close(editor):
            self.notebook.forget(editor)
            if not self.notebook.tabs():
                self._new_document()

    def _text_action(self, action):
        editor = self.current_editor()
        if not editor:
            return
        try:
            getattr(editor.text, action)()
        except tk.TclError:
            self.bell()

    def _text_event(self, sequence):
        editor = self.current_editor()
        if editor:
            editor.text.event_generate(sequence)
            editor.text.focus_set()

    def find_in_editor(self):
        editor = self.current_editor()
        if not editor:
            return
        query = simpledialog.askstring("Find in source", "Find:", initialvalue=self.last_search, parent=self)
        if not query:
            return
        self.last_search = query
        editor.text.tag_remove("search", "1.0", "end")
        editor.text.tag_configure("search", background="#725f23", foreground="#ffffff")
        start = editor.text.index("insert")
        found = editor.text.search(query, start, stopindex="end", nocase=True)
        if not found:
            found = editor.text.search(query, "1.0", stopindex=start, nocase=True)
        if found:
            end = f"{found}+{len(query)}c"
            editor.text.tag_add("search", found, end)
            editor.text.mark_set("insert", end)
            editor.text.see(found)
            editor.text.focus_set()
        else:
            self.status_left.set(f"No match for {query!r}")

    def _source_path_for_action(self, editor):
        if editor.path and not editor.modified:
            return editor.path, None
        if editor.modified:
            choice = messagebox.askyesnocancel(
                "Unsaved source", "Save changes before running? Choose No to run a temporary copy.", parent=self
            )
            if choice is None:
                return None, None
            if choice and not self.save_current():
                return None, None
            if choice:
                return editor.path, None
        directory = editor.path.parent if editor.path else self.project_dir
        try:
            with tempfile.NamedTemporaryFile(
                mode="w", encoding="utf-8", newline="\n", suffix=".tell",
                prefix=".tell-ide-", dir=directory, delete=False,
            ) as handle:
                handle.write(editor.content())
                return Path(handle.name), Path(handle.name)
        except OSError as exc:
            messagebox.showerror("Cannot run source", str(exc), parent=self)
            return None, None

    def _start_process(self, kind, command, cwd, temporary=None):
        if not command:
            self.shell.write("This action requires the native Tell runtime. Install the Tell executable or set TELL_NATIVE_EXECUTABLE.\n", "error")
            return None
        if kind == "run" and self.run_id is not None:
            self.stop_program()
        process_id = self.next_process_id
        self.next_process_id += 1
        options = {
            "cwd": str(cwd), "stdin": subprocess.PIPE,
            "stdout": subprocess.PIPE, "stderr": subprocess.PIPE,
            "bufsize": 0, "env": child_environment(self.root_dir),
        }
        if os.name == "nt":
            options["creationflags"] = getattr(subprocess, "CREATE_NO_WINDOW", 0)
        try:
            process = subprocess.Popen(command, **options)
        except (OSError, ValueError) as exc:
            if temporary:
                temporary.unlink(missing_ok=True)
            self.shell.write(f"Could not start Tell: {exc}\n", "error")
            return None
        self.processes[process_id] = (process, kind, temporary)
        if kind == "repl":
            self.repl_id = process_id
        elif kind == "run":
            self.run_id = process_id
            self.shell.prompt.set("input")
        readers = []
        for stream, tag in ((process.stdout, None), (process.stderr, "error")):
            worker = threading.Thread(target=self._read_output, args=(process_id, stream, tag), daemon=True)
            worker.start()
            readers.append(worker)
        threading.Thread(target=self._wait_process, args=(process_id, process, readers), daemon=True).start()
        self.status_left.set(f"Running {' '.join(command)}")
        return process_id

    def _read_output(self, process_id, stream, tag):
        decoder = codecs.getincrementaldecoder("utf-8")("replace")
        reader = getattr(stream, "read1", stream.read)
        try:
            while True:
                chunk = reader(4096)
                if not chunk:
                    break
                text = decoder.decode(chunk)
                if text:
                    self.events.put(("output", process_id, text, tag))
            tail = decoder.decode(b"", final=True)
            if tail:
                self.events.put(("output", process_id, tail, tag))
        except (OSError, ValueError):
            pass
        finally:
            try:
                stream.close()
            except OSError:
                pass

    def _wait_process(self, process_id, process, readers):
        try:
            code = process.wait()
        except OSError:
            code = -1
        for worker in readers:
            worker.join()
        self.events.put(("finished", process_id, code, None))

    def _poll_events(self):
        if self._closing:
            return
        while True:
            try:
                event = self.events.get_nowait()
            except queue.Empty:
                break
            if event[0] == "output":
                _, process_id, text, tag = event
                if process_id in self.processes:
                    self.shell.write(text, tag)
            else:
                _, process_id, code, _unused = event
                record = self.processes.pop(process_id, None)
                if record:
                    _process, kind, temporary = record
                    if temporary:
                        temporary.unlink(missing_ok=True)
                    if kind == "repl" and self.repl_id == process_id:
                        self.repl_id = None
                        self.shell.block_depth = 0
                        self.shell.prompt.set(">>>")
                        self.shell.write(f"\nTell shell stopped (exit {code}). Enter a line to start a fresh session.\n", "system")
                    else:
                        if self.run_id == process_id:
                            self.run_id = None
                            self.shell.prompt.set("..." if self.shell.block_depth else ">>>")
                        self.shell.write(f"\n[{kind} finished with exit code {code}]\n", "system" if code == 0 else "error")
                        self.status_left.set("Ready" if code == 0 else f"{kind.title()} exited with code {code}")
        self.after(40, self._poll_events)

    def restart_shell(self):
        if self.repl_id is not None:
            record = self.processes.get(self.repl_id)
            if record:
                try:
                    record[0].kill()
                except OSError:
                    pass
        command, backend = command_for("repl", root=self.root_dir)
        self.backend_label.set(backend)
        self.shell.write(f"\nTell IDE shell · {backend}\n", "system")
        process_id = self._start_process("repl", command, self.project_dir)
        if process_id is not None:
            self.shell.block_depth = 0
            self.shell.prompt.set(">>>")
            self.shell.input.focus_set()

    def send_shell_line(self, line):
        process_id = self.run_id if self.run_id in self.processes else self.repl_id
        record = self.processes.get(process_id) if process_id is not None else None
        if record is None:
            if self.run_id is None:
                self.restart_shell()
                process_id = self.repl_id
                record = self.processes.get(process_id) if process_id is not None else None
        if record:
            try:
                record[0].stdin.write((line + "\n").encode("utf-8"))
                record[0].stdin.flush()
            except (OSError, BrokenPipeError, AttributeError) as exc:
                self.shell.write(f"\nInput could not be sent: {exc}\n", "error")

    def _editor_action(self, operation):
        editor = self.current_editor()
        if not editor:
            return
        source_path, temporary = self._source_path_for_action(editor)
        if source_path is None:
            return
        command, backend = command_for(operation, source_path, root=self.root_dir)
        self.backend_label.set(backend)
        if operation == "run":
            self.shell.write(f"\n▶ Run {editor.path.name if editor.path else 'untitled.tell'}\n", "system")
        else:
            self.shell.write(f"\n$ tell {operation} {source_path}\n", "command")
        self._start_process(operation, command, source_path.parent, temporary)

    def run_current(self):
        self._editor_action("run")

    def check_current(self):
        self._editor_action("check")

    def build_current(self):
        editor = self.current_editor()
        if not editor:
            return
        if editor.path is None or editor.modified:
            if not self.save_current():
                return
        destination = editor.path.with_suffix(".tbc")
        if destination.exists() and not messagebox.askyesno("Replace bytecode?", f"Replace {destination.name}?", parent=self):
            return
        command, backend = command_for("build", editor.path, output=destination, root=self.root_dir)
        self.backend_label.set(backend)
        self.shell.write(f"\n$ tell build {editor.path} --output {destination}\n", "command")
        self._start_process("build", command, editor.path.parent)

    def run_project_tests(self):
        editor = self.current_editor()
        directory = editor.path.parent if editor and editor.path else self.project_dir
        command, backend = command_for("test", directory, root=self.root_dir)
        self.backend_label.set(backend)
        self.shell.write(f"\n$ tell test {directory}\n", "command")
        self._start_process("test", command, directory)

    def stop_program(self):
        if self.run_id is None:
            self.shell.write("No Tell program is currently running.\n", "system")
            return
        record = self.processes.get(self.run_id)
        if record:
            self.shell.write("\n[Stopping Tell program…]\n", "system")
            try:
                record[0].terminate()
            except OSError:
                pass

    def open_language_reference(self):
        path = self.root_dir / "docs" / "LANGUAGE_SPECIFICATION.md" if self.root_dir else None
        if path and path.exists():
            webbrowser.open(path.as_uri())
        else:
            webbrowser.open("https://github.com/KernelGhost-gif/Tell-Code")

    def open_ide_guide(self):
        path = self.root_dir / "docs" / "IDE.md" if self.root_dir else None
        if path and path.exists():
            webbrowser.open(path.as_uri())
        else:
            self.shell.write("Install the Tell source package to view its IDE guide.\n", "system")

    def about(self):
        messagebox.showinfo(
            "About Tell IDE",
            f"Tell IDE {__version__}\nA sentence-first editor and interactive shell.\n\nRun with F5 · Check with F7 · Build with Ctrl+B",
            parent=self,
        )

    def close(self):
        for tab_id in list(self.notebook.tabs()):
            editor = self.nametowidget(tab_id)
            if not self._confirm_editor_close(editor):
                return
        self._closing = True
        for process, _kind, _temporary in self.processes.values():
            try:
                process.kill()
            except OSError:
                pass
        self.destroy()


def main() -> int:
    if tk is None:
        print(
            f"Tell IDE needs Tkinter, which is not installed with this Python ({_TK_ERROR}). "
            "Install your operating system's Python Tk package and try again.",
            file=sys.stderr,
        )
        return 2
    application = TellIDE()
    application.mainloop()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
