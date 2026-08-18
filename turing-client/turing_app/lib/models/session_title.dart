import 'session.dart';

/// How an untitled conversation is named on screen.
///
/// A session has no title until the backend derives one from its first
/// message, so "untitled" means "not started yet" — not "something went
/// wrong". Shared rather than duplicated because the sidebar and search
/// results show the same conversations, and two different placeholders for
/// one session read as two different sessions.
String sessionDisplayTitle(Session session) =>
    session.title?.isNotEmpty == true ? session.title! : 'New chat';
