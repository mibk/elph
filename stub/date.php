<?php

interface DateTimeInterface
{
	const ATOM    = "Y-m-d\\TH:i:sP";
	const COOKIE  = "l, d-M-Y H:i:s T";
	const ISO8601 = "Y-m-d\\TH:i:sO";
	const RFC822  = "D, d M y H:i:s O";
	const RFC850  = "l, d-M-y H:i:s T";
	const RFC1036 = "D, d M y H:i:s O";
	const RFC1123 = "D, d M Y H:i:s O";
	const RFC7231 = "D, d M Y H:i:s \\G\\M\\T";
	const RFC2822 = "D, d M Y H:i:s O";
	const RFC3339 = "Y-m-d\\TH:i:sP";
	const RSS     = "D, d M Y H:i:s O";
	const W3C     = "Y-m-d\\TH:i:sP";

	const ISO8601_EXPANDED = "X-m-d\\TH:i:sP";
	const RFC3339_EXTENDED = "Y-m-d\\TH:i:s.vP";

	function diff(DateTimeInterface $targetObject, bool $absolute = false): DateInterval;
	function format(string $format): string;
	function getOffset(): int;
	function getTimestamp(): int;
	function getTimezone(): DateTimeZone|false;
}

class DateTime implements DateTimeInterface
{
	static function createFromFormat(string $format, string $datetime): DateTime|false;

	static function createFromImmutable(DateTimeImmutable $object): DateTime;

	function modify(string $modifier): static;

	function diff(DateTimeInterface $targetObject, bool $absolute = false): DateInterval;

	function format(string $format): string;

	function getTimestamp(): int;

	function setTimestamp(int $timestamp);

	function setDate(int $year, int $month, int $day): DateTime;

	function setTime(int $hour, int $minute, int $second = 0, int $microsecond = 0): DateTime;

	function getLastErrors(): array|false;

	function setTimezone(DateTimeZone $timezone): DateTime;
}

class DateTimeImmutable implements DateTimeInterface
{
	static function createFromFormat(string $format, string $datetime): DateTimeImmutable|false;

	static function createFromMutable(DateTime $object): DateTimeImmutable;

	function createFromInterface(DateTimeInterface $object): DateTimeImmutable;

	function modify(string $modifier): static;

	function diff(DateTimeInterface $targetObject, bool $absolute = false): DateInterval;

	function format(string $format): string;

	function getOffset(): int;

	function getTimestamp(): int;

	function getTimezone(): DateTimeZone|false;

	function getLastErrors(): array|false;

	function setDate(int $year, int $month, int $day): DateTimeImmutable;

	function setTime(int $hour, int $minute, int $second = 0, int $microsecond = 0): DateTimeImmutable;

	function setTimestamp(int $timestamp): DateTimeImmutable;

	function setTimezone(DateTimeZone $timezone): DateTimeImmutable;
}

class DateInterval
{
	public int $y;
	public int $m;
	public int $d;
	public int $h;
	public int $i;
	public int $s;
	public float $f;
	public int $invert;
	public mixed $days;
	public bool $from_string;
	public string $date_string;

	function format(string $format): string;
}

class IntlDateFormatter
{
	const FULL;
	const LONG;
	const MEDIUM;
	const SHORT;
	const NONE;
	const RELATIVE_FULL;
	const RELATIVE_LONG;
	const RELATIVE_MEDIUM;
	const RELATIVE_SHORT;
	const GREGORIAN;
	const TRADITIONAL;

	function format(IntlCalendar|DateTimeInterface|array|string|int|float $datetime): string|false;

	function setPattern(string $pattern): bool;
}

class DatePeriod
{
}

class DateTimeZone
{
	const AFRICA;
	const AMERICA;
	const ANTARCTICA;
	const ARCTIC;
	const ASIA;
	const ATLANTIC;
	const AUSTRALIA;
	const EUROPE;
	const INDIAN;
	const PACIFIC;
	const UTC;
	const ALL;
	const ALL_WITH_BC;
	const PER_COUNTRY;
}
