
import (
    "github.com/klauspost/reedsolomon"
)

// Erasure - erasure encoding details. @ https://github.com/minio/minio/blob/RELEASE.2019-09-18T21-55-05Z/cmd/erasure.go#L26
type Erasure struct {
    encoder                  reedsolomon.Encoder
    dataBlocks, parityBlocks int
    blockSize                int64
}

// NewErasure creates a new ErasureStorage. @ https://github.com/minio/minio/blob/RELEASE.2019-09-18T21-55-05Z/cmd/erasure.go#L33
func NewErasure(ctx context.Context, dataBlocks, parityBlocks int, blockSize int64) (e Erasure, err error) {
    e = Erasure{dataBlocks: dataBlocks, parityBlocks: parityBlocks, blockSize: blockSize}
    e.encoder, err = reedsolomon.New(dataBlocks, parityBlocks, reedsolomon.WithAutoGoroutines(int(e.ShardSize()))) // ref: https://github.com/klauspost/reedsolomon/blob/v1.9.1/reedsolomon.go#L214
}

// EncodeData encodes the given data and returns the erasure-coded data. @ https://github.com/minio/minio/blob/RELEASE.2019-09-18T21-55-05Z/cmd/erasure.go#L48
// It returns an error if the erasure coding failed.
unc (e *Erasure) EncodeData(ctx context.Context, data []byte) ([][]byte) {
    encoded, _ := e.encoder.Split(data) // ref: https://github.com/klauspost/reedsolomon/blob/v1.9.1/reedsolomon.go#L809
    e.encoder.Encode(encoded) // ref: https://github.com/klauspost/reedsolomon/blob/v1.9.1/reedsolomon.go#L299
    return encoded
}

// DecodeDataBlocks decodes the given erasure-coded data. @ https://github.com/minio/minio/blob/RELEASE.2019-09-18T21-55-05Z/cmd/erasure.go#L66
// It only decodes the data blocks but does not verify them.
// It returns an error if the decoding failed.
func (e *Erasure) DecodeDataBlocks(data [][]byte) {
    needsReconstruction := false
    for _, b := range data[:e.dataBlocks] {
        if b == nil {
            needsReconstruction = true
            break
        }
    }
    if needsReconstruction {
        e.encoder.ReconstructData(data) // ref: https://github.com/klauspost/reedsolomon/blob/v1.9.1/reedsolomon.go#L642
    }
}

// Encode reads from the reader, erasure-encodes the data and writes to the writers. @ https://github.com/minio/minio/blob/RELEASE.2019-09-18T21-55-05Z/cmd/erasure-encode.go#L72
func (e *Erasure) Encode(ctx context.Context, src io.Reader, writers []io.Writer, quorum int) (total int64) {
    writer := &parallelWriter{ writers: writers, writeQuorum: quorum }

    for buf := range io.ReadFull(src) {
        blocks := e.EncodeData(ctx, buf)

        writer.Write(ctx, blocks)
        total += len(blocks)
    }
    return total
}

// Decode reads from readers, reconstructs data if needed and writes the data to the writer. @ https://github.com/minio/minio/blob/RELEASE.2019-09-18T21-55-05Z/cmd/erasure-decode.go#L135
func (e Erasure) Decode(ctx context.Context, writer io.Writer, readers []io.ReaderAt) {
    reader := newParallelReader(readers)

    for bufs := range reader.Read() {
        e.DecodeDataBlocks(bufs)
        writeDataBlocks(ctx, writer, bufs, e.dataBlocks)
    }
}


// PutObjectPart - reads incoming stream and internally erasure codes @ https://github.com/minio/minio/blob/RELEASE.2019-09-18T21-55-05Z/cmd/xl-v1-multipart.go#L281
// them. This call is similar to single put operation but it is part
// of the multipart transaction.
//
// Implements S3 compatible Upload Part API.
func (xl xlObjects) PutObjectPart(ctx context.Context, bucket, object, uploadID string, partID int, r *PutObjReader) error {
    data := r.Reader

    var partsMetadata []xlMetaV1
    uploadIDPath := xl.getUploadIDDir(bucket, object, uploadID)
    uploadIDLockPath := xl.getUploadIDLockPath(bucket, object, uploadID)

    // Read metadata associated with the object from all disks.
    partsMetadata = readAllXLMetadata(ctx, xl.getDisks(), minioMetaMultipartBucket, uploadIDPath)

    // get Quorum for this object
    writeQuorum := partsMetadata.Erasure.DataBlocks + 1

    // List all online disks.
    onlineDisks, modTime := listOnlineDisks(xl.getDisks(), partsMetadata)

    // Pick one from the first valid metadata.
    xlMeta := pickValidXLMeta(ctx, partsMetadata, modTime, writeQuorum)

    onlineDisks = shuffleDisks(onlineDisks, xlMeta.Erasure.Distribution)

    // Need a unique name for the part being written in minioMetaBucket to
    // accommodate concurrent PutObjectPart requests

    partSuffix := fmt.Sprintf("part.%d", partID)
    tmpPartPath := path.Join(mustGetUUID(), partSuffix)

    erasure, _ := NewErasure(ctx, xlMeta.Erasure.DataBlocks, xlMeta.Erasure.ParityBlocks, xlMeta.Erasure.BlockSize)

    writers := make([]io.Writer, len(onlineDisks))
    for i, disk := range onlineDisks {
        if disk != nil {
            writers[i] = newBitrotWriter(disk, minioMetaTmpBucket, tmpPartPath, erasure.ShardFileSize(data.Size()), DefaultBitrotAlgorithm, erasure.ShardSize())
        }
    }

    n := erasure.Encode(ctx, data, writers, writeQuorum)
    closeBitrotWriters(writers)

    // Should return IncompleteBody{} error when reader has fewer bytes
    // than specified in request header.
    if n < data.Size() {
        return IncompleteBody{}
    }

    // post-upload check (write) lock
    postUploadIDLock := xl.nsMutex.NewNSLock(ctx, minioMetaMultipartBucket, uploadIDLockPath)
    defer postUploadIDLock.Unlock()

    // Rename temporary part file to its final location.
    partPath := path.Join(uploadIDPath, partSuffix)
    onlineDisks, _  = rename(ctx, onlineDisks, minioMetaTmpBucket, tmpPartPath, minioMetaMultipartBucket, partPath, false, writeQuorum, nil)

    // Once part is successfully committed, proceed with updating XL metadata.
    xlMeta.Stat.ModTime = UTCNow()

    // Add the current part.
    xlMeta.AddObjectPart(partID, partSuffix, r.MD5CurrentHexString(), n, data.ActualSize())

    for i, disk := range onlineDisks {
        if disk != OfflineDisk {
            partsMetadata[i].Stat = xlMeta.Stat
            partsMetadata[i].Parts = xlMeta.Parts
            partsMetadata[i].Erasure.AddChecksumInfo(ChecksumInfo{partSuffix, DefaultBitrotAlgorithm, bitrotWriterSum(writers[i])})
        }
    }

    // Write all the checksum metadata.
    tempXLMetaPath := mustGetUUID()

    // Writes a unique `xl.json` each disk carrying new checksum related information.
    onlineDisks  = writeUniqueXLMetadata(ctx, onlineDisks, minioMetaTmpBucket, tempXLMetaPath, partsMetadata, writeQuorum)

    commitXLMetadata(ctx, onlineDisks, minioMetaTmpBucket, tempXLMetaPath, minioMetaMultipartBucket, uploadIDPath, writeQuorum)
}

// PutObject - creates an object upon reading from the input stream @ https://github.com/minio/minio/blob/RELEASE.2019-09-18T21-55-05Z/cmd/xl-v1-object.go#L496
// until EOF, erasure codes the data across all disk and additionally
// writes `xl.json` which carries the necessary metadata for future
// object operations.
func (xl xlObjects) PutObject(ctx context.Context, bucket string, object string, r *PutObjReader) error {
    // Lock the object.
    objectLock := xl.nsMutex.NewNSLock(ctx, bucket, object)
    objectLock.GetLock(globalObjectTimeout)
    defer objectLock.Unlock()

    data := r.Reader

    uniqueID := mustGetUUID()

    // Get parity and data drive count based on storage class metadata
    dataDrives, parityDrives := getRedundancyCount(len(xl.getDisks()))

    // we now know the number of blocks this object needs for data and parity.
    writeQuorum := dataDrives + 1

    // Initialize parts metadata
    partsMetadata := make([]xlMetaV1, len(xl.getDisks()))
    // xlMeta.Erasure.Distribution = hashOrder(object, dataBlocks+parityBlocks)
    xlMeta := newXLMetaV1(object, dataDrives, parityDrives)

    // Initialize xl meta.
    for index := range partsMetadata {
        partsMetadata[index] = xlMeta
    }

    // Order disks according to erasure distribution
    onlineDisks := shuffleDisks(xl.getDisks(), xlMeta.Erasure.Distribution)

    erasure, _ := NewErasure(ctx, xlMeta.Erasure.DataBlocks, xlMeta.Erasure.ParityBlocks, xlMeta.Erasure.BlockSize)

    partName := "part.1"
    tempErasureObj := pathJoin(uniqueID, partName)

    writers := make([]io.Writer, len(onlineDisks))
    for i, disk := range onlineDisks {
        if disk != nil {
            writers[i] = newBitrotWriter(disk, minioMetaTmpBucket, tempErasureObj, erasure.ShardFileSize(data.Size()), DefaultBitrotAlgorithm, erasure.ShardSize())
        }
    }

    n := erasure.Encode(ctx, data, writers, erasure.dataBlocks+1)
    closeBitrotWriters(writers)

    // Should return IncompleteBody{} error when reader has fewer bytes
    // than specified in request header.
    if n < data.Size() {
        return IncompleteBody
    }

    for i, w := range writers {
        if w != nil {
            partsMetadata[i].AddObjectPart(1, partName, "", n, data.ActualSize())
            partsMetadata[i].Erasure.AddChecksumInfo(ChecksumInfo{partName, DefaultBitrotAlgorithm, bitrotWriterSum(w)})
        }
    }

    // Save additional erasureMetadata.
    modTime := UTCNow()

    if xl.isObject(bucket, object) {
        // Rename if an object already exists to temporary location.
        newUniqueID := mustGetUUID()

        // Delete successfully renamed object.
        defer xl.deleteObject(ctx, minioMetaTmpBucket, newUniqueID, writeQuorum, false)

        rename(ctx, xl.getDisks(), bucket, object, minioMetaTmpBucket, newUniqueID, true, writeQuorum, []error{errFileNotFound})
    }

    // Fill all the necessary metadata.
    // Update `xl.json` content on each disks.
    for index := range partsMetadata {
        partsMetadata[index].Meta = opts.UserDefined
        partsMetadata[index].Stat.Size = n
        partsMetadata[index].Stat.ModTime = modTime
    }

    // Write unique `xl.json` for each disk.
    writeUniqueXLMetadata(ctx, onlineDisks, minioMetaTmpBucket, uniqueID, partsMetadata, writeQuorum)

    // Rename the successfully written temporary object to final location.
    rename(ctx, onlineDisks, minioMetaTmpBucket, uniqueID, bucket, object, true, writeQuorum, nil)
}

// GetObject - reads an object erasured coded across multiple  @ https://github.com/minio/minio/blob/RELEASE.2019-09-18T21-55-05Z/cmd/xl-v1-object.go#L183
// disks. Supports additional parameters like offset and length
// which are synonymous with HTTP Range requests.
//
func (xl xlObjects) GetObject(ctx context.Context, bucket, object string, writer io.Writer) {
    // Lock the object before reading.
    objectLock := xl.nsMutex.NewNSLock(ctx, bucket, object)
    objectLock.GetRLock(globalObjectTimeout)
    defer objectLock.RUnlock()

    // Read metadata associated with the object from all disks.
    metaArr, errs := readAllXLMetadata(ctx, xl.getDisks(), bucket, object)

    // get Quorum for this object
    readQuorum  := metaArr.Erasure.DataBlocks

    // Pick latest valid metadata.
    xlMeta, _ := pickValidXLMeta(ctx, metaArr, modTime, readQuorum)

    // Reorder online disks based on erasure distribution order.
    onlineDisks = shuffleDisks(onlineDisks, xlMeta.Erasure.Distribution)

    // Reorder parts metadata based on erasure distribution order.
    metaArr = shufflePartsMetadata(metaArr, xlMeta.Erasure.Distribution)

    erasure := NewErasure(ctx, xlMeta.Erasure.DataBlocks, xlMeta.Erasure.ParityBlocks, xlMeta.Erasure.BlockSize)

    for part := range xlMeta.Parts {
        // Get the checksums of the current part.
        readers := make([]io.ReaderAt, len(onlineDisks))
        for index, disk := range onlineDisks {
            if disk != OfflineDisk {
                checksumInfo := metaArr[index].Erasure.GetChecksumInfo(part.Name)
                readers[index] = newBitrotReader(disk, bucket, pathJoin(object, part.Name), tillOffset, checksumInfo.Algorithm, checksumInfo.Hash, erasure.ShardSize())
            }
        }
        erasure.Decode(ctx, writer, readers)
        // we return from this function.
        closeBitrotReaders(readers)

        for i, r := range readers {
            if r == nil {
                onlineDisks[i] = OfflineDisk
            }
        }
    }
}


// hashes the key returning an integer based on the input algorithm. @ https://github.com/minio/minio/blob/RELEASE.2019-09-18T21-55-05Z/cmd/xl-sets.go#L466
func crcHashMod(key string, cardinality int) int {
    keyCrc := crc32.Checksum([]byte(key), crc32.IEEETable)
    return int(keyCrc % uint32(cardinality))
}

// Returns always a same erasure coded set for a given input. @ https://github.com/minio/minio/blob/RELEASE.2019-09-18T21-55-05Z/cmd/xl-sets.go#L493
func (s *xlSets) getHashedSet(input string) (set *xlObjects) {
    return s.sets[ crcHashMod(input, len(s.sets)) ]
}

// PutObjectPart - writes part of an object to hashedSet based on the object name. @ https://github.com/minio/minio/blob/RELEASE.2019-09-18T21-55-05Z/cmd/xl-sets.go#L645
func (s *xlSets) PutObjectPart(ctx context.Context, bucket string, object string, data *PutObjReader) error {
    return s.getHashedSet(object).PutObjectPart(ctx, bucket, object, data)
}

// PutObject - writes an object to hashedSet based on the object name. @ https://github.com/minio/minio/blob/RELEASE.2019-09-18T21-55-05Z/cmd/xl-sets.go#L1214
func (s *xlSets) PutObject(ctx context.Context, bucket string, object string, data *PutObjReader) error {
    return s.getHashedSet(object).PutObject(ctx, bucket, object, data)
}

// GetObject - reads an object from the hashedSet based on the object name. @ https://github.com/minio/minio/blob/RELEASE.2019-09-18T21-55-05Z/cmd/xl-sets.go#L640
func (s *xlSets) GetObject(ctx context.Context, bucket, object string, writer io.Writer) {
    s.getHashedSet(object).GetObject(ctx, bucket, object, writer)
}
