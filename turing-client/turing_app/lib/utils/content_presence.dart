bool hasDisplayableContent(String content) {
  for (final scalar in content.runes) {
    if (!_isApprovedWhitespace(scalar)) {
      return true;
    }
  }
  return false;
}

bool _isApprovedWhitespace(int scalar) {
  return (scalar >= 0x0009 && scalar <= 0x000D) ||
      scalar == 0x0020 ||
      scalar == 0x0085 ||
      scalar == 0x00A0 ||
      scalar == 0x1680 ||
      (scalar >= 0x2000 && scalar <= 0x200A) ||
      scalar == 0x2028 ||
      scalar == 0x2029 ||
      scalar == 0x202F ||
      scalar == 0x205F ||
      scalar == 0x3000;
}
