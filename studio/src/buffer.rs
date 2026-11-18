use std::{ fs, io, path::{Path, PathBuf} };

/// UTF-8 source buffer with a byte-safe caret and a single-file save target.
#[derive(Debug, Clone)]
pub struct SourceBuffer { path: PathBuf, text: String, cursor: usize, dirty: bool }

impl SourceBuffer {
    pub fn new(path: Option<PathBuf>) -> io::Result<Self> {
        let path = path.unwrap_or_else(|| PathBuf::from("untitled.tell"));
        let text = match fs::read_to_string(&path) {
            Ok(contents) => contents,
            Err(error) if error.kind() == io::ErrorKind::NotFound => String::new(),
            Err(error) => return Err(error),
        };
        let cursor = text.len();
        Ok(Self { path, text, cursor, dirty: false })
    }
    pub fn path(&self) -> &Path { &self.path }
    pub fn text(&self) -> &str { &self.text }
    pub fn is_dirty(&self) -> bool { self.dirty }
    pub fn editor_text(&self) -> String { let mut value = self.text.clone(); value.insert_str(self.cursor, "▏"); value }
    pub fn insert(&mut self, value: &str) {
        if value.is_empty() { return; }
        self.text.insert_str(self.cursor, value);
        self.cursor += value.len();
        self.dirty = true;
    }
    pub fn backspace(&mut self) {
        if self.cursor == 0 { return; }
        let start = self.text[..self.cursor].char_indices().next_back().map(|(index, _)| index).unwrap_or(0);
        self.text.replace_range(start..self.cursor, "");
        self.cursor = start;
        self.dirty = true;
    }
    pub fn move_left(&mut self) {
        if self.cursor == 0 { return; }
        self.cursor = self.text[..self.cursor].char_indices().next_back().map(|(index, _)| index).unwrap_or(0);
    }
    pub fn move_right(&mut self) {
        if self.cursor >= self.text.len() { return; }
        let width = self.text[self.cursor..].chars().next().map(char::len_utf8).unwrap_or(0);
        self.cursor = (self.cursor + width).min(self.text.len());
    }
    pub fn save(&mut self) -> io::Result<&Path> {
        fs::write(&self.path, self.text.as_bytes())?;
        self.dirty = false;
        Ok(&self.path)
    }
}

#[cfg(test)]
mod tests {
    use super::SourceBuffer;
    use std::{fs, path::PathBuf, time::{SystemTime, UNIX_EPOCH}};
    fn unique_path() -> PathBuf {
        let nonce = SystemTime::now().duration_since(UNIX_EPOCH).unwrap().as_nanos();
        std::env::temp_dir().join(format!("tell-studio-{nonce}.tell"))
    }
    #[test]
    fn editing_is_utf8_boundary_safe_and_marks_dirty() {
        let path = unique_path();
        let mut buffer = SourceBuffer::new(Some(path.clone())).unwrap();
        buffer.insert("λTell");
        assert_eq!(buffer.text(), "λTell");
        buffer.move_left();
        buffer.backspace();
        assert_eq!(buffer.text(), "λTel");
        assert!(buffer.is_dirty());
        let _ = fs::remove_file(path);
    }
    #[test]
    fn save_and_reload_round_trip_source() {
        let path = unique_path();
        let mut buffer = SourceBuffer::new(Some(path.clone())).unwrap();
        buffer.insert("say \"hello\"\n");
        buffer.save().unwrap();
        assert!(!buffer.is_dirty());
        let reloaded = SourceBuffer::new(Some(path.clone())).unwrap();
        assert_eq!(reloaded.text(), "say \"hello\"\n");
        let _ = fs::remove_file(path);
    }
}
