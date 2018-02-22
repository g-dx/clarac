#include <stdlib.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>

// ---------------------------------------------------------------------------------------------------------------------
// C Runtime functions
// ---------------------------------------------------------------------------------------------------------------------

void indexOutOfBounds(int index)
{
    printf("\n// -------------------------------------------------------------------------------------------\n");
    printf("// Crash: Index (%d) is out of bounds!\n", index);
    printf("// -------------------------------------------------------------------------------------------\n");
    printf("\nBacktrace:\nTODO!\n\n");
    // TODO: Output stacktrace
    abort();
}

// ---------------------------------------------------------------------------------------------------------------------
// lib/arrays.clara
// ---------------------------------------------------------------------------------------------------------------------

// Casting support
intptr_t toIntArray(intptr_t arrayHeader) { return arrayHeader; }
intptr_t toByteArray(intptr_t arrayHeader) { return arrayHeader; }
intptr_t toHeader(intptr_t pointer) { return pointer; }

// ---------------------------------------------------------------------------------------------------------------------
// lib/strings.clara
// ---------------------------------------------------------------------------------------------------------------------

// Casting support
// NOTE: codegen always increments the pointer by 8 when passing an array or string to an external function. This allows
// printf to work. As such reverse that when returning the same pointer!
intptr_t asString(intptr_t byteArray) { return byteArray - 8; }
