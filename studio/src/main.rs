mod buffer;
use buffer::SourceBuffer;
use gpui::{App, Bounds, Context, FocusHandle, KeyDownEvent, Render, Window, WindowBounds, WindowOptions, div, prelude::*, px, rgb, size};
use gpui_platform::application;
use std::{env, path::PathBuf, process::Command};

struct Studio { focus_handle: FocusHandle, buffer: SourceBuffer, status: String, output: String }

impl Studio {
    fn new(_window: &mut Window, cx: &mut Context<Self>, source_path: Option<PathBuf>) -> Self {
        match SourceBuffer::new(source_path) {
            Ok(buffer) => Self { focus_handle: cx.focus_handle(), buffer, status: "Ready · Ctrl/⌘+S to save · Ctrl/⌘+R to run".into(), output: String::new() },
            Err(error) => Self { focus_handle: cx.focus_handle(), buffer: SourceBuffer::new(None).expect("create an empty Tell buffer"), status: format!("Could not open file: {error}"), output: String::new() },
        }
    }
    fn save(&mut self) {
        match self.buffer.save() {
            Ok(path) => self.status = format!("Saved {}", path.display()),
            Err(error) => self.status = format!("Save failed: {error}"),
        }
    }
    fn run_current(&mut self, cx: &mut Context<Self>) {
        let path = match self.buffer.save() {
            Ok(path) => path.to_path_buf(),
            Err(error) => { self.status = format!("Save before run failed: {error}"); cx.notify(); return; }
        };
        let executable = env::var_os("TELL_NATIVE_EXECUTABLE").unwrap_or_else(|| "tell".into());
        self.status = format!("Running {}", path.display());
        self.output.clear();
        cx.spawn(async move |this, cx| {
            let command_path = path.clone();
            let result = cx.background_executor().spawn(async move {
                Command::new(executable).arg("run").arg(&command_path).output()
            }).await;
            let (status, output) = match result {
                Ok(result) => {
                    let mut text = String::from_utf8_lossy(&result.stdout).into_owned();
                    text.push_str(&String::from_utf8_lossy(&result.stderr));
                    if result.status.success() { (format!("Finished {}", path.display()), text) }
                    else { (format!("Tell exited with {}", result.status), text) }
                }
                Err(error) => (format!("Could not start the Go runtime: {error}"), "Set TELL_NATIVE_EXECUTABLE or put the native tell binary on PATH.".into()),
            };
            let _ = this.update(cx, |view, cx| { view.status = status; view.output = output; cx.notify(); });
        }).detach();
        cx.notify();
    }
    fn on_key_down(&mut self, event: &KeyDownEvent, _window: &mut Window, cx: &mut Context<Self>) {
        let key = event.keystroke.key.to_ascii_lowercase();
        let modifiers = &event.keystroke.modifiers;
        if modifiers.control || modifiers.platform {
            match key.as_str() { "s" => self.save(), "r" => self.run_current(cx), _ => return }
            cx.notify(); return;
        }
        match key.as_str() {
            "backspace" => self.buffer.backspace(),
            "left" => self.buffer.move_left(),
            "right" => self.buffer.move_right(),
            "enter" | "return" => self.buffer.insert("\n"),
            "tab" => self.buffer.insert("    "),
            _ => { if modifiers.alt { return; } if let Some(value) = event.keystroke.key_char.as_deref() { self.buffer.insert(value); } else { return; } }
        }
        self.status = if self.buffer.is_dirty() { format!("{} · unsaved", self.buffer.path().display()) } else { "Ready".into() };
        cx.notify();
    }
    fn toolbar_button(label: &'static str, action: impl Fn(&mut Self, &mut Context<Self>) + 'static, cx: &mut Context<Self>) -> impl IntoElement {
        div().id(label).px_3().py_2().rounded_md().bg(rgb(0x263449)).text_color(rgb(0xe7edf5)).cursor_pointer().hover(|style| style.bg(rgb(0x354a65))).child(label).on_click(cx.listener(move |this, _, _, cx| action(this, cx)))
    }
}

impl Render for Studio {
    fn render(&mut self, _window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let save = Self::toolbar_button("Save", |this, cx| { this.save(); cx.notify(); }, cx);
        let run = Self::toolbar_button("Run", |this, cx| this.run_current(cx), cx);
        let file_name = self.buffer.path().display().to_string();
        let editor_text = self.buffer.editor_text();
        let output = if self.output.is_empty() { "Run output appears here.".to_string() } else { self.output.clone() };
        div().id("tell-studio").size_full().flex().flex_col().gap_3().p_4().bg(rgb(0x101722)).text_color(rgb(0xe7edf5))
            .child(div().flex().items_center().justify_between()
                .child(div().flex().flex_col().gap_1()
                    .child(div().text_xl().font_weight(gpui::FontWeight::BOLD).child("Tell Studio"))
                    .child(div().text_sm().text_color(rgb(0x9aa9bd)).child(file_name)))
                .child(div().flex().gap_2().child(save).child(run)))
            .child(div().flex_1().min_h(px(280.)).p_3().rounded_md().border_1().border_color(rgb(0x35465b)).bg(rgb(0x172231))
                .track_focus(&self.focus_handle)
                .on_key_down(cx.listener(Self::on_key_down))
                .on_click(cx.listener(|this, _, window, cx| { window.focus(&this.focus_handle, cx); }))
                .child(editor_text))
            .child(div().h(px(26.)).text_sm().text_color(rgb(0xa5b5c8)).child(self.status.clone()))
            .child(div().h(px(112.)).p_3().rounded_md().border_1().border_color(rgb(0x263449)).bg(rgb(0x0c121b)).child(output))
    }
}

fn main() {
    let source_path = env::args_os().nth(1).map(PathBuf::from);
    application().run(move |cx: &mut App| {
        let bounds = Bounds::centered(None, size(px(1100.), px(760.)), cx);
        let source_path = source_path.clone();
        cx.open_window(WindowOptions { window_bounds: Some(WindowBounds::Windowed(bounds)), ..Default::default() },
            |window, cx| cx.new(|cx| Studio::new(window, cx, source_path.clone())))
            .expect("failed to open Tell Studio window");
        cx.activate(true);
    });
}
