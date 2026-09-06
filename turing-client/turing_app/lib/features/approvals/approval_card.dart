import 'dart:async';

import 'package:flutter/material.dart';

import '../../l10n/generated/app_localizations.dart';
import '../../models/approval.dart';

class ApprovalCard extends StatefulWidget {
  const ApprovalCard({
    super.key,
    required this.approvalId,
    required this.toolName,
    required this.onApprove,
    required this.onDeny,
    this.loadDetails,
    this.busy = false,
  });

  final String approvalId;
  final String toolName;
  final Future<ApprovalDetails> Function(bool refresh)? loadDetails;
  final ValueChanged<ApprovalDetails> onApprove;
  final VoidCallback onDeny;
  final bool busy;

  @override
  State<ApprovalCard> createState() => _ApprovalCardState();
}

class _ApprovalCardState extends State<ApprovalCard> {
  ApprovalDetails? _details;
  bool _loading = false;
  bool _failed = false;
  bool _expired = false;
  int _readVersion = 0;
  Timer? _expiryTimer;

  @override
  void initState() {
    super.initState();
    unawaited(_load(false));
  }

  @override
  void didUpdateWidget(ApprovalCard oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.approvalId != widget.approvalId) {
      _readVersion++;
      _loading = false;
      unawaited(_load(false));
    }
  }

  Future<void> _load(bool refresh) async {
    if (widget.busy || _loading) return;
    _expiryTimer?.cancel();
    final loader = widget.loadDetails;
    if (loader == null) {
      setState(() {
        _details = null;
        _failed = true;
      });
      return;
    }
    final version = ++_readVersion;
    setState(() {
      _loading = true;
      _details = null;
      _failed = false;
      _expired = false;
    });
    try {
      final details = await loader(refresh);
      if (!mounted || version != _readVersion) return;
      if (details.approvalId != widget.approvalId ||
          details.toolName != widget.toolName) {
        throw const FormatException('Approval detail identity mismatch');
      }
      setState(() {
        _details = details;
        _loading = false;
      });
      final expiry = details.expiresAt;
      if (expiry != null && expiry.isAfter(DateTime.now())) {
        _expiryTimer = Timer(expiry.difference(DateTime.now()), () {
          if (mounted) setState(() => _expired = true);
        });
      }
    } on Exception {
      if (!mounted || version != _readVersion) return;
      setState(() {
        _loading = false;
        _failed = true;
      });
    }
  }

  String _stateCopy(AppLocalizations l10n, ApprovalDetails details) {
    if (_expired ||
        (details.status == 'pending' &&
            details.expiresAt != null &&
            !DateTime.now().isBefore(details.expiresAt!))) {
      return l10n.approvalExpired;
    }
    return switch (details.previewState) {
      ApprovalPreviewState.ready => l10n.approvalReviewInstructions,
      ApprovalPreviewState.unsupported => l10n.approvalUnsupported,
      ApprovalPreviewState.redacted => l10n.approvalRedacted,
      ApprovalPreviewState.oversized => l10n.approvalOversized,
      ApprovalPreviewState.binary => l10n.approvalBinary,
      ApprovalPreviewState.expired => l10n.approvalExpired,
      ApprovalPreviewState.stale => l10n.approvalStale,
      ApprovalPreviewState.terminal => l10n.approvalTerminal,
      ApprovalPreviewState.unknown ||
      ApprovalPreviewState.unavailable => l10n.approvalUnavailable,
    };
  }

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final details = _details;
    final file = details?.filePreview;
    final canApprove =
        !widget.busy &&
        !_loading &&
        !_failed &&
        !_expired &&
        details != null &&
        details.canApproveAt(DateTime.now());
    return ConstrainedBox(
      constraints: BoxConstraints(
        maxHeight: MediaQuery.sizeOf(context).height * 0.55,
      ),
      child: Card(
        margin: const EdgeInsets.fromLTRB(12, 8, 12, 4),
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            mainAxisSize: MainAxisSize.min,
            children: [
              Flexible(
                fit: FlexFit.loose,
                child: Scrollbar(
                  child: SingleChildScrollView(
                    primary: false,
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Text(
                          l10n.approvalRequested(widget.toolName),
                          style: Theme.of(context).textTheme.titleMedium,
                        ),
                        const SizedBox(height: 8),
                        if (_loading)
                          Semantics(
                            liveRegion: true,
                            label: l10n.approvalLoading,
                            child: const SizedBox.square(
                              dimension: 20,
                              child: CircularProgressIndicator(strokeWidth: 2),
                            ),
                          )
                        else if (_failed || details == null)
                          Text(l10n.approvalUnavailable)
                        else ...[
                          Semantics(
                            liveRegion: true,
                            child: Text(_stateCopy(l10n, details)),
                          ),
                          if (details.previewState ==
                                  ApprovalPreviewState.ready ||
                              details.previewState ==
                                  ApprovalPreviewState.unsupported) ...[
                            if (file != null) ...[
                              SelectableText(
                                l10n.approvalTarget(file.physicalPath),
                              ),
                              Text(
                                file.beforeExists
                                    ? l10n.approvalExistingFile
                                    : l10n.approvalAbsentFile,
                              ),
                              SelectableText(
                                file.unifiedDiff,
                                style: const TextStyle(fontFamily: 'monospace'),
                              ),
                              ExpansionTile(
                                title: Text(l10n.approvalBeforeAfter),
                                children: [
                                  SelectableText(
                                    l10n.approvalBefore(file.beforeText),
                                  ),
                                  SelectableText(
                                    l10n.approvalAfter(file.afterText),
                                  ),
                                ],
                              ),
                            ] else if (details.argumentsJson.isNotEmpty)
                              SelectableText(
                                details.argumentsJson,
                                style: const TextStyle(fontFamily: 'monospace'),
                              ),
                            ExpansionTile(
                              title: Text(l10n.approvalBinding),
                              children: [
                                SelectableText(
                                  l10n.approvalArgumentsHash(details.argsHash),
                                ),
                                SelectableText(
                                  l10n.approvalPreviewHash(details.previewHash),
                                ),
                                if (file != null) ...[
                                  SelectableText(
                                    l10n.approvalBeforeHash(file.beforeHash),
                                  ),
                                  SelectableText(
                                    l10n.approvalAfterHash(file.afterHash),
                                  ),
                                ],
                              ],
                            ),
                          ],
                        ],
                      ],
                    ),
                  ),
                ),
              ),
              const SizedBox(height: 12),
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  FilledButton.icon(
                    onPressed: canApprove
                        ? () {
                            if (!_expired &&
                                details.canApproveAt(DateTime.now())) {
                              widget.onApprove(details);
                            }
                          }
                        : null,
                    icon: const Icon(Icons.check),
                    label: Text(l10n.approvalApprove),
                  ),
                  OutlinedButton.icon(
                    onPressed:
                        widget.busy || (details != null && !details.canDeny)
                        ? null
                        : widget.onDeny,
                    icon: const Icon(Icons.close),
                    label: Text(l10n.approvalDeny),
                  ),
                  if (!_loading &&
                      widget.loadDetails != null &&
                      (details == null || details.status == 'pending'))
                    TextButton(
                      onPressed: widget.busy ? null : () => _load(true),
                      child: Text(
                        details?.previewState == ApprovalPreviewState.stale
                            ? l10n.approvalRefresh
                            : l10n.approvalRetry,
                      ),
                    ),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }

  @override
  void dispose() {
    _readVersion++;
    _expiryTimer?.cancel();
    super.dispose();
  }
}
