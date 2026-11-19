# Books are processed by a separate offline tool

All PDF processing (building the Compendium from core books, and turning Adventure Books into Adventures, including extraction, translation and the review queue) happens in a separate command-line tool run on the owner's machine. The game server never receives a PDF: it only loads versioned, reviewed Compendium and Adventure files. This removes untrusted-file upload and parsing from the internet-facing server, keeps the heavy extraction and translation cost out of the game, and fits private use (ADR 0003).
