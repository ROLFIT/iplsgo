#include <oci.h>

// define simple way to respresent Oracle version
#define ORACLE_VERSION(major, minor) \
        ((major << 8) | minor)

// define what version of Oracle we are building as 2 byte hex number
#if !defined(OCI_MAJOR_VERSION) && defined(OCI_ATTR_MODULE)
#define OCI_MAJOR_VERSION 10
#define OCI_MINOR_VERSION 1
#endif

#if defined(OCI_MAJOR_VERSION) && defined(OCI_MINOR_VERSION)
#define ORACLE_VERSION_HEX \
        ORACLE_VERSION(OCI_MAJOR_VERSION, OCI_MINOR_VERSION)
#else
#error Unsupported version of OCI.
#endif

#if ORACLE_VERSION_HEX >= ORACLE_VERSION(12,1)
    #define OCIBINDBYNAME               OCIBindByName2
    #define OCIBINDBYPOS                OCIBindByPos2
    #define OCIDEFINEBYPOS              OCIDefineByPos2
    #define ACTUAL_LENGTH_TYPE          ub4
	#define MAX_BINARY_BYTES			32767
	#define LENGTH_TYPE					sb8
#else
    #define OCIBINDBYNAME               OCIBindByName
    #define OCIBINDBYPOS                OCIBindByPos
    #define OCIDEFINEBYPOS              OCIDefineByPos
    #define ACTUAL_LENGTH_TYPE          ub2
	#define MAX_BINARY_BYTES			32767 //4000
	#define LENGTH_TYPE					sb4
#endif

#if ORACLE_VERSION_HEX >= ORACLE_VERSION(10,1)
	#define LOB_LENGTH_TYPE				oraub8
	#define OCILOBGETLENGTH				OCILobGetLength2
	#define OCILOBTRIM 					OCILobTrim2
	#define OCILOBWRITE					OCILobWrite2
#else
	#define LOB_LENGTH_TYPE				ub4
	#define OCILOBGETLENGTH				OCILobGetLength
	#define OCILOBTRIM 					OCILobTrim
	#define OCILOBWRITE					OCILobWrite
#endif
