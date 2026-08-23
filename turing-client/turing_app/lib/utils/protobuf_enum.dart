import 'package:protobuf/protobuf.dart';

T decodeClosedEnum<T>({
  required GeneratedMessage message,
  required int fieldNumber,
  required T Function() readValue,
  required T unknownValue,
}) {
  final field = message.unknownFields.getField(fieldNumber);
  if (field != null && field.varints.isNotEmpty) {
    return unknownValue;
  }
  return readValue();
}
